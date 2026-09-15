/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package testcrypto

import (
	"testing"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/hyperledger/fabric-x-common/protoutil"
)

// TestPrepareInPlaceMatchesClone is the check the InPlace and ReuseDataHash options rest on: they exist
// to make preparation cheap, and they are only worth having if the block that comes out is the one that
// would have come out anyway. Anything else would change what a committer receives while claiming to
// change only what it costs to produce.
func TestPrepareInPlaceMatchesClone(t *testing.T) {
	t.Parallel()
	prev := PrepareBlockHeaderAndMetadata(block(t, 4), BlockPrepareParameters{})
	params := BlockPrepareParameters{PrevBlock: prev, LastConfigBlockIndex: 1}

	slow := PrepareBlockHeaderAndMetadata(block(t, 4), params)

	// What the fast path's caller does: hash the data itself, one stage earlier, and hand over a block
	// it will not touch again.
	own := block(t, 4)
	own.Header = &common.BlockHeader{DataHash: protoutil.ComputeBlockDataHash(own.Data)}
	fastParams := params
	fastParams.InPlace = true
	fastParams.ReuseDataHash = true
	fast := PrepareBlockHeaderAndMetadata(own, fastParams)

	require.Empty(t, cmpDiff(slow, fast))
	// In place means in place: the block returned is the block passed in, not a copy of it.
	require.Same(t, own, fast)
}

// TestReuseDataHashWithoutOneComputesIt covers the misuse: a caller that asks to reuse the hash but
// never supplied one gets a correct block rather than one carrying an empty hash.
func TestReuseDataHashWithoutOneComputesIt(t *testing.T) {
	t.Parallel()
	want := PrepareBlockHeaderAndMetadata(block(t, 3), BlockPrepareParameters{})
	got := PrepareBlockHeaderAndMetadata(block(t, 3), BlockPrepareParameters{ReuseDataHash: true})
	require.NotEmpty(t, got.Header.DataHash)
	require.Empty(t, cmpDiff(want, got))
}

func block(t *testing.T, txCount int) *common.Block {
	t.Helper()
	data := make([][]byte, txCount)
	for i := range data {
		data[i] = protoutil.MarshalOrPanic(&common.Envelope{Payload: []byte{byte(i), 0x2a}})
	}
	return &common.Block{Data: &common.BlockData{Data: data}}
}

func cmpDiff(want, got *common.Block) string {
	if proto.Equal(want, got) {
		return ""
	}
	return "prepared blocks differ:\nwant " + want.String() + "\ngot  " + got.String()
}
