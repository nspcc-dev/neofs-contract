package common

type Ballot struct {
	// ID of the voting decision.
	ID []byte

	// Public keys of already voted inner ring nodes.
	Voters [][]byte

	// Height of block with the last vote.
	Height int
}
