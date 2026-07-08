package common

import (
	"github.com/nspcc-dev/neo-go/pkg/interop"
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

func InitVote() {
	SetSerialized([]byte(voteKey), []Ballot{})
}

// Vote adds ballot for the decision with a specific 'id' and returns the amount
// of unique voters for that decision.
func Vote(id, from []byte) int {
	var (
		newCandidates []Ballot
		candidates    = getBallots()
		found         = -1
		blockHeight   = ledger.CurrentIndex()
	)

	for _, cnd := range candidates {
		if blockHeight-cnd.Height > blockDiff {
			continue
		}

		if bytesEqual(cnd.ID, id) {
			voters := cnd.Voters

			for j := range voters {
				if bytesEqual(voters[j], from) {
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

	SetSerialized([]byte(voteKey), newCandidates)

	return found
}

// RemoveVotes clears ballots of the decision that has been accepted by
// inner ring nodes.
func RemoveVotes(id []byte) {
	var (
		candidates = getBallots()
		index      int
	)

	for i, cnd := range candidates {
		if bytesEqual(cnd.ID, id) {
			index = i
			break
		}
	}

	util.Remove(candidates, index)
	SetSerialized([]byte(voteKey), candidates)
}

// getBallots returns a deserialized slice of vote ballots.
func getBallots() []Ballot {
	data := storage.LocalGet([]byte(voteKey))
	if data != nil {
		return std.Deserialize(data.([]byte)).([]Ballot)
	}

	return []Ballot{}
}

// bytesEqual compares two slices of bytes by wrapping them into strings,
// which is necessary with new util.Equals interop behaviour, see neo-go#1176.
func bytesEqual(a []byte, b []byte) bool {
	return util.Equals(string(a), string(b))
}
