package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/invoker"
	"github.com/nspcc-dev/neofs-contract/rpc/container"
	"github.com/nspcc-dev/neofs-contract/rpc/netmap"
	"github.com/nspcc-dev/neofs-contract/rpc/nns"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/urfave/cli"
)

func initClient(addr string, name string) (*rpcclient.Client, uint32, error) {
	c, err := rpcclient.New(context.Background(), addr, rpcclient.Options{})
	if err != nil {
		return nil, 0, fmt.Errorf("RPC %s: %w", name, err)
	}
	err = c.Init()
	if err != nil {
		return nil, 0, fmt.Errorf("RPC %s init: %w", name, err)
	}
	h, err := c.GetBlockCount()
	if err != nil {
		return nil, 0, fmt.Errorf("RPC %s block count: %w", name, err)
	}
	return c, h, nil
}

func getFSContent(c *rpcclient.Client) ([][]byte, []*netmap.NetmapNode, error) {
	nnsState, err := c.GetContractStateByID(nns.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get NNS state: %w", err)
	}
	inv := invoker.New(c, nil)

	nnsReader := nns.NewReader(inv, nnsState.Hash)
	containerH, err := nnsReader.ResolveFSContract(nns.NameContainer)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve container contract: %w", err)
	}
	reader := container.NewReader(inv, containerH)
	containers, err := reader.List([]byte{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list containers: %w", err)
	}

	netmapH, err := nnsReader.ResolveFSContract(nns.NameNetmap)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve netmap contract: %w", err)
	}
	netmapReader := netmap.NewReader(inv, netmapH)
	netmap, err := netmapReader.Netmap()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve netmap: %w", err)
	}
	return containers, netmap, nil
}

func cliMain(c *cli.Context) error {
	a := c.Args().Get(0)
	b := c.Args().Get(1)
	if a == "" {
		return errors.New("no arguments given")
	}
	if b == "" {
		return errors.New("missing second argument")
	}
	ca, ha, err := initClient(a, "A")
	if err != nil {
		return err
	}
	cb, hb, err := initClient(b, "B")
	if err != nil {
		return err
	}
	if ha != hb {
		var diff = hb - ha
		if ha > hb {
			diff = ha - hb
		}
		if diff > 10 && !c.Bool("ignore-height") { // Allow some height drift.
			return fmt.Errorf("chains have different heights: %d vs %d", ha, hb)
		}
	}
	fmt.Printf("RPC %s height: %d\nRPC %s height: %d\n", a, ha, b, hb)

	containersA, netmapA, err := getFSContent(ca)
	if err != nil {
		return fmt.Errorf("RPC %s: %w", a, err)
	}
	containersB, netmapB, err := getFSContent(cb)
	if err != nil {
		return fmt.Errorf("RPC %s: %w", b, err)
	}

	var (
		errTxt                     string
		containersDiff, netmapDiff int
	)
	if len(containersA) != len(containersB) {
		errTxt = fmt.Sprintf("number of containers mismatch: %d vs %d", len(containersA), len(containersB))
	} else {
		fmt.Printf("number of containers checked: %d\n", len(containersA))
		for i := range containersA {
			if !bytes.Equal(containersA[i], containersB[i]) {
				containersDiff++
				dumpContentDiff("container", i, a, b, containersA[i], containersB[i])
			}
		}
	}
	if containersDiff != 0 {
		if len(errTxt) != 0 {
			errTxt += "; "
		}
		errTxt += fmt.Sprintf("%d containers mismatch", containersDiff)
	}

	if len(netmapA) != len(netmapB) {
		if len(errTxt) != 0 {
			errTxt += "; "
		}
		errTxt += fmt.Sprintf("number of netmap entries mismatch: %d vs %d", len(netmapA), len(netmapB))
	} else {
		fmt.Printf("number of netmap entries checked: %d\n", len(netmapA))
		for i := range netmapA {
			if netmapA[i].State.Cmp(netmapB[i].State) != 0 || !bytes.Equal(netmapA[i].BLOB, netmapB[i].BLOB) {
				netmapDiff++
				dumpContentDiff("netmap entry", i, a, b, netmapA[i], netmapB[i])
			}
		}
	}
	if netmapDiff != 0 {
		if len(errTxt) != 0 {
			errTxt += "; "
		}
		errTxt += fmt.Sprintf("%d netmap entries mismatch", netmapDiff)
	}

	if len(errTxt) != 0 {
		return errors.New(errTxt)
	}
	return nil
}

func dumpContentDiff(itemName string, i int, a string, b string, itemA any, itemB any) {
	fmt.Printf("%s %d:\n", itemName, i)
	da := spew.Sdump(itemA)
	db := spew.Sdump(itemB)
	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(da),
		B:        difflib.SplitLines(db),
		FromFile: a,
		ToFile:   b,
		Context:  1,
	})
	fmt.Println(diff)
}

func main() {
	ctl := cli.NewApp()
	ctl.Name = "compare-fscontent"
	ctl.Version = "1.0"
	ctl.Usage = "compare-fscontent RPC_A RPC_B"
	ctl.Action = cliMain
	ctl.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "ignore-height, g",
			Usage: "ignore height difference",
		},
	}

	if err := ctl.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
