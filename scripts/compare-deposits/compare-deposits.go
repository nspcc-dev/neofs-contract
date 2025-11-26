package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/nspcc-dev/neo-go/pkg/encoding/address"
	"github.com/nspcc-dev/neo-go/pkg/encoding/fixedn"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/gas"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/invoker"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/nep17"
	"github.com/nspcc-dev/neo-go/pkg/util"
	"github.com/nspcc-dev/neo-go/pkg/vm/stackitem"
	"github.com/nspcc-dev/neofs-contract/rpc/nns"
)

func initClient(addr string, name string) (*rpcclient.Client, error) {
	c, err := rpcclient.New(context.Background(), addr, rpcclient.Options{})
	if err != nil {
		return nil, fmt.Errorf("RPC %s: %w", name, err)
	}
	err = c.Init()
	if err != nil {
		return nil, fmt.Errorf("RPC %s init: %w", name, err)
	}
	return c, nil
}

type deposit struct {
	from   string
	amount *big.Int
	tx     util.Uint256
}

type mint struct {
	tx        util.Uint256
	to        util.Uint160
	amount    int64
	depositTx util.Uint256
	data      []byte
}

func getSupply(c *rpcclient.Client, h util.Uint160, height uint32) int64 {
	inv := invoker.NewHistoricAtHeight(height, c, nil)
	n17 := nep17.NewReader(inv, h)
	s, err := n17.TotalSupply()
	if err != nil {
		fmt.Printf("WARN: cannot get historic supply for %d height: %s\n", height, err)
		return 0
	}
	return s.Int64()
}

func cliMain() error {
	flag.Parse()

	args := flag.Args()
	if len(args) < 3 {
		return errors.New("usage: program <RPC_MAINNET> <NEOFS_CONTRACT> <RPC_FSCHAIN>")
	}

	rpcMainAddress := args[0]
	neoFSContractAddress := args[1]
	rpcFSAddress := args[2]

	fsCont, err := address.StringToUint160(neoFSContractAddress)
	if err != nil {
		return fmt.Errorf("bad contract address: %w", err)
	}
	cMain, err := initClient(rpcMainAddress, "main")
	if err != nil {
		return err
	}
	cFS, err := initClient(rpcFSAddress, "FS")
	if err != nil {
		return err
	}

	now := uint64(time.Now().Unix()) * 1000 // Milliseconds.

	var (
		deposits []deposit
		mints    []mint
	)
	for i := 0; ; i++ {
		var from uint64
		var limit = 100
		trans, err := cMain.GetNEP17Transfers(fsCont, &from, &now, &limit, &i)
		if err != nil {
			return fmt.Errorf("can't get transfers: %w", err)
		}
		for _, d := range trans.Received {
			amount, err := fixedn.FromString(d.Amount, 8)
			if err != nil {
				return fmt.Errorf("amount conversion error: %w", err)
			}
			deposits = append(deposits, deposit{d.Address, amount, d.TxHash})
		}
		if len(trans.Received)+len(trans.Sent) < 100 {
			break
		}
	}
	slices.Reverse(deposits)

	gasR := gas.NewReader(invoker.New(cMain, nil))
	bMain, err := gasR.BalanceOf(fsCont)
	if err != nil {
		return err
	}
	fmt.Println(len(deposits), "deposits, main balance:", fixedn.ToString(bMain, 8))

	nnsHash, err := nns.InferHash(cFS)
	if err != nil {
		return err
	}
	nnsR := nns.NewReader(invoker.New(cFS, nil), nnsHash)
	balanceHash, err := nnsR.ResolveFSContract("balance")
	if err != nil {
		return err
	}

	maxH, err := cFS.GetBlockCount()
	if err != nil {
		return err
	}
	maxH-- // blockCount to height

	var (
		actualSupply = getSupply(cFS, balanceHash, maxH)
		knownBlocks  = make(map[uint32]int64)

		currHeight uint32 = 1
		currSupply        = getSupply(cFS, balanceHash, currHeight)
	)

	fmt.Printf("FS supply at %d height: %s\n", maxH, fixedn.ToString(big.NewInt(actualSupply), 12))
	for currHeight < maxH {
		searchedSegment := int(maxH - currHeight)
		n := sort.Search(searchedSegment, func(i int) bool {
			var h = currHeight + uint32(i)
			s, ok := knownBlocks[h]
			if !ok {
				s = getSupply(cFS, balanceHash, h)
				knownBlocks[h] = s
			}
			return currSupply != s
		})
		if n == searchedSegment {
			break
		}
		currHeight += uint32(n)
		currSupply = knownBlocks[currHeight]
		fmt.Println("FS block", currHeight-1, "supply", fixedn.ToString(big.NewInt(currSupply), 12))
		b, err := cFS.GetBlockByIndex(currHeight - 1)
		if err != nil {
			return err
		}
		for _, t := range b.Transactions {
			l, err := cFS.GetApplicationLog(t.Hash(), nil)
			if err != nil {
				return err
			}
			for _, e := range l.Executions[0].Events {
				if e.ScriptHash.Equals(balanceHash) && e.Name == "TransferX" {
					itms := e.Item.Value().([]stackitem.Item)
					_, ok := itms[0].(stackitem.Null)
					if !ok { // Non-mint.
						continue
					}
					var to util.Uint160
					toB, _ := itms[1].TryBytes()
					if len(toB) == 20 {
						to, _ = util.Uint160DecodeBytesBE(toB)
					}
					amount, _ := itms[2].TryInteger()
					data, _ := itms[3].TryBytes()
					var h util.Uint256
					if len(data) == 33 {
						h, _ = util.Uint256DecodeBytesBE(data[1:33])
					} else if len(data) == 32 {
						h, _ = util.Uint256DecodeBytesBE(data)
					}
					mints = append(mints, mint{t.Hash(), to, amount.Int64(), h, data})
				}
			}
		}
	}
	failedDeposits := slices.Clone(deposits)
	unmatchedMints := slices.Clone(mints)
	for _, m := range mints {
		// Won't detect duplicated mints for the same deposit.
		failedDeposits = slices.DeleteFunc(failedDeposits, func(d deposit) bool {
			return d.tx.Equals(m.depositTx)
		})
	}
	for _, d := range deposits {
		unmatchedMints = slices.DeleteFunc(unmatchedMints, func(m mint) bool {
			return d.tx.Equals(m.depositTx)
		})
	}

	for _, d := range failedDeposits {
		fmt.Println("0x"+d.tx.StringLE(), d.from, fixedn.ToString(d.amount, 8))
	}
	for _, m := range unmatchedMints {
		fmt.Println("0x"+m.tx.StringLE(), address.Uint160ToString(m.to), m.amount, m.depositTx.StringLE(), m.data)
	}

	return nil
}

func main() {
	if err := cliMain(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
