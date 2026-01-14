package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/davecgh/go-spew/spew"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient"
	"github.com/nspcc-dev/neo-go/pkg/rpcclient/invoker"
	"github.com/nspcc-dev/neofs-contract/rpc/container"
	"github.com/nspcc-dev/neofs-contract/rpc/netmap"
	"github.com/nspcc-dev/neofs-contract/rpc/nns"
	"github.com/pmezard/go-difflib/difflib"
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

func getFSContent(c *rpcclient.Client) ([][]byte, []netmap.NetmapNode2, error) {
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
	sess, iter, err := reader.ContainersOf([]byte{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list containers: %w", err)
	}
	defer inv.TerminateSession(sess)
	var containers [][]byte
	items, err := inv.TraverseIterator(sess, &iter, 0)
	for ; err == nil && len(items) > 0; items, err = inv.TraverseIterator(sess, &iter, 0) {
		for _, item := range items {
			b, err := item.TryBytes()
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get bytes for container: %w", err)
			}
			containers = append(containers, b)
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to traverse containers: %w", err)
	}

	netmapH, err := nnsReader.ResolveFSContract(nns.NameNetmap)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve netmap contract: %w", err)
	}
	netmapReader := netmap.NewReader(inv, netmapH)
	sess, iter, err = netmapReader.ListNodes()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve netmap: %w", err)
	}
	var nm []netmap.NetmapNode2
	items, err = inv.TraverseIterator(sess, &iter, 0)
	for ; err == nil && len(items) > 0; items, err = inv.TraverseIterator(sess, &iter, 0) {
		for _, item := range items {
			var n netmap.NetmapNode2
			err := n.FromStackItem(item)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to convert a node: %w", err)
			}
			nm = append(nm, n)
		}
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to traverse nodes: %w", err)
	}
	return containers, nm, nil
}

func cliMain() error {
	var ignoreHeightFlag bool
	flag.BoolVar(&ignoreHeightFlag, "ignore-height", false, "ignore height difference")
	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		return errors.New("usage: program [--ignore-height] <FIRST_RPC_NODE> <SECOND_RPC_NODE>")
	}

	firstNodeAddress := args[0]
	secondNodeAddress := args[1]

	ca, ha, err := initClient(firstNodeAddress, "A")
	if err != nil {
		return err
	}
	cb, hb, err := initClient(secondNodeAddress, "B")
	if err != nil {
		return err
	}
	if ha != hb {
		var diff = hb - ha
		if ha > hb {
			diff = ha - hb
		}
		if diff > 10 && !ignoreHeightFlag { // Allow some height drift.
			return fmt.Errorf("chains have different heights: %d vs %d", ha, hb)
		}
	}
	fmt.Printf("RPC %s height: %d\nRPC %s height: %d\n", firstNodeAddress, ha, secondNodeAddress, hb)

	containersA, netmapA, err := getFSContent(ca)
	if err != nil {
		return fmt.Errorf("RPC %s: %w", firstNodeAddress, err)
	}
	containersB, netmapB, err := getFSContent(cb)
	if err != nil {
		return fmt.Errorf("RPC %s: %w", secondNodeAddress, err)
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
				dumpContentDiff("container", i, firstNodeAddress, secondNodeAddress, containersA[i], containersB[i])
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
			if netmapA[i].State.Cmp(netmapB[i].State) != 0 || netmapA[i].Key.Cmp(netmapB[i].Key) != 0 ||
				!maps.Equal(netmapA[i].Attributes, netmapB[i].Attributes) ||
				!slices.Equal(netmapA[i].Addresses, netmapB[i].Addresses) {
				netmapDiff++
				dumpContentDiff("netmap entry", i, firstNodeAddress, secondNodeAddress, netmapA[i], netmapB[i])
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
	if err := cliMain(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
