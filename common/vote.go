package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop/binary"
	"github.com/nspcc-dev/neo-go/pkg/interop/blockchain"
	"github.com/nspcc-dev/neo-go/pkg/interop/crypto"
	"github.com/nspcc-dev/neo-go/pkg/interop/storage"
	"github.com/nspcc-dev/neo-go/pkg/interop/util"
)

type Ballot struct {
	// ID of the voting decision.
	ID []byte

	// Public keys of already voted inner ring nodes.
	Voters [][]byte

	// Height of block with the last vote.
	Height int
}

const voteKey = "ballots"

const blockDiff = 20 // change base on performance evaluation

func InitVote(ctx storage.Context) {
	SetSerialized(ctx, voteKey, []Ballot{})
}

// Vote adds ballot for the decision with specific 'id' and returns amount
// on unique voters for that decision.
func Vote(ctx storage.Context, id, from []byte) int {
	var (
		newCandidates []Ballot
		candidates    = getBallots(ctx)
		found         = -1
		blockHeight   = blockchain.GetHeight()
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
		voters := [][]byte{from}
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
		newCandidates []Ballot
		candidates    = getBallots(ctx)
	)

	for i := 0; i < len(candidates); i++ {
		cnd := candidates[i]
		if !BytesEqual(cnd.ID, id) {
			newCandidates = append(newCandidates, cnd)
		}
	}

	SetSerialized(ctx, voteKey, newCandidates)
}

// getBallots returns deserialized slice of vote ballots.
func getBallots(ctx storage.Context) []Ballot {
	data := storage.Get(ctx, voteKey)
	if data != nil {
		return binary.Deserialize(data.([]byte)).([]Ballot)
	}

	return []Ballot{}
}

// BytesEqual compares two slice of bytes by wrapping them into strings,
// which is necessary with new util.Equal interop behaviour, see neo-go#1176.
func BytesEqual(a []byte, b []byte) bool {
	return util.Equals(string(a), string(b))
}

func InvokeID(args []interface{}, prefix []byte) []byte {
	for i := range args {
		arg := args[i].([]byte)
		prefix = append(prefix, arg...)
	}

	return crypto.SHA256(prefix)
}
