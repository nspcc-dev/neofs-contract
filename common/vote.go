package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/ledger"
	"github.com/nspcc-dev/neo-go/pkg/interop/native/std"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
)

type Ballot struct {
	// ID of the voting decision.
	ID []byte

	// Public keys of the already voted inner ring nodes.
	Voters []interop.PublicKey

	// Height of block with the last vote.
	Height int
}

const voteKey = "ballots"

const blockDiff = 20 // change base on performance evaluation

func InitVote(ctx storage.Context) {
	SetSerialized(ctx, voteKey, []Ballot{})
}

// Vote adds ballot for the decision with a specific 'id' and returns the amount
// of unique voters for that decision.
func Vote(ctx storage.Context, id, from []byte) int {
	var (
		newCandidates []Ballot
		candidates    = getBallots(ctx)
		found         = -1
		blockHeight   = ledger.CurrentIndex()
	)

	for i := 0; i < len(candidates); i++ {
		cnd := candidates[i]

		if blockHeight-cnd.Height > blockDiff {
			continue
		}

		if BytesEqual(cnd.ID, id) {
			voters := cnd.Voters

			for j := range voters {
				if BytesEqual(voters[j], from) {
					return len(voters)
				}
			}

			voters = append(voters, from)
			cnd = Ballot{ID: id, Voters: voters, Height: blockHeight}
			found = len(voters)
		}

		newCandidates = append(newCandidates, cnd)
	}

	if found < 0 {
		voters := []interop.PublicKey{from}
		newCandidates = append(newCandidates, Ballot{
			ID:     id,
			Voters: voters,
			Height: blockHeight})
		found = 1
	}

	SetSerialized(ctx, voteKey, newCandidates)

	return found
}

// RemoveVotes clears ballots of the decision that has been accepted by
// inner ring nodes.
func RemoveVotes(ctx storage.Context, id []byte) {
	var (
		candidates = getBallots(ctx)
		index      int
	)

	for i := 0; i < len(candidates); i++ {
		cnd := candidates[i]
		if BytesEqual(cnd.ID, id) {
			index = i
			break
		}
	}

	util.Remove(candidates, index)
	SetSerialized(ctx, voteKey, candidates)
}

// getBallots returns a deserialized slice of vote ballots.
func getBallots(ctx storage.Context) []Ballot {
	data := storage.Get(ctx, voteKey)
	if data != nil {
		return std.Deserialize(data.([]byte)).([]Ballot)
	}

	return []Ballot{}
}

// BytesEqual compares two slices of bytes by wrapping them into strings,
// which is necessary with new util.Equals interop behaviour, see neo-go#1176.
func BytesEqual(a []byte, b []byte) bool {
	return util.Equals(string(a), string(b))
}

// InvokeID returns hashed value of prefix and args concatenation. Iy is used to
// identify different ballots.
func InvokeID(args []interface{}, prefix []byte) []byte {
	for i := range args {
		arg := args[i].([]byte)
		prefix = append(prefix, arg...)
	}

	return crypto.Sha256(prefix)
}

/*
   Check if the invocation is made from known container or audit contracts.
   This is necessary because calls from these contracts require to do transfer
   without signature collection (1 invoke transfer).

   IR1, IR2, IR3, IR4 -(4 invokes)-> [ Container Contract ] -(1 invoke)-> [ Balance Contract ]

   We can do 1 invoke transfer if:
   - invokation has happened from inner ring node,
   - it is indirect invocation from another smart-contract.

   However, there is a possible attack, when a malicious inner ring node creates
   a malicious smart-contract in the morph chain to do indirect call.

   MaliciousIR -(1 invoke)-> [ Malicious Contract ] -(1 invoke)-> [ Balance Contract ]

   To prevent that, we have to allow 1 invoke transfer from authorised well-known
   smart-contracts, that will be set up at `Init` method.
*/

func FromKnownContract(ctx storage.Context, caller interop.Hash160, key string) bool {
	addr := storage.Get(ctx, key).(interop.Hash160)
	return caller.Equals(addr)
}
