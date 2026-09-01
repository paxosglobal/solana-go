// Copyright 2026 github.com/gagliardetto
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package solana

import (
	"testing"

	bin "github.com/gagliardetto/binary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// v1Message builds a small but non-trivial v1 message: an inline compute
// budget (config), two account keys, and one instruction.
func v1Message(t *testing.T) *Message {
	t.Helper()
	m := &Message{version: MessageVersionV1}
	m.Header = MessageHeader{
		NumRequiredSignatures:       1,
		NumReadonlySignedAccounts:   0,
		NumReadonlyUnsignedAccounts: 1,
	}
	m.RecentBlockhash = Hash{1, 2, 3, 4}
	m.AccountKeys = PublicKeySlice{
		{9, 9, 9},
		SystemProgramID,
	}
	m.Instructions = []CompiledInstruction{{
		ProgramIDIndex: 1,
		Accounts:       []uint16{0},
		Data:           Base58{7, 8, 9},
	}}
	m.TransactionConfig = TransactionConfig{}.
		WithPriorityFee(5000).
		WithComputeUnitLimit(200_000)
	return m
}

// TestMessageV1_BinaryRoundTrip asserts a v1 message marshals to the SIMD-0385
// wire format (0x81 prefix) and decodes back to the same fields. This is the
// path Chain Scanner's downstream consumers exercise once Solana v1
// transactions activate; before this support the fork forced any versioned
// prefix to v0 and could not decode a v1 body.
func TestMessageV1_BinaryRoundTrip(t *testing.T) {
	in := v1Message(t)

	encoded, err := in.MarshalBinary()
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
	assert.Equal(t, messageVersionV1Prefix, encoded[0], "v1 message must start with the 0x81 prefix")

	var out Message
	require.NoError(t, out.UnmarshalWithDecoder(bin.NewBinDecoder(encoded)))

	assert.Equal(t, MessageVersionV1, out.GetVersion())
	assert.Equal(t, in.Header, out.Header)
	assert.Equal(t, in.RecentBlockhash, out.RecentBlockhash)
	assert.Equal(t, in.AccountKeys, out.AccountKeys)
	require.Len(t, out.Instructions, 1)
	assert.Equal(t, in.Instructions[0].ProgramIDIndex, out.Instructions[0].ProgramIDIndex)
	assert.Equal(t, in.Instructions[0].Accounts, out.Instructions[0].Accounts)
	assert.Equal(t, []byte(in.Instructions[0].Data), []byte(out.Instructions[0].Data))
	require.NotNil(t, out.TransactionConfig.PriorityFee)
	assert.Equal(t, uint64(5000), *out.TransactionConfig.PriorityFee)
	require.NotNil(t, out.TransactionConfig.ComputeUnitLimit)
	assert.Equal(t, uint32(200_000), *out.TransactionConfig.ComputeUnitLimit)
	assert.Nil(t, out.TransactionConfig.HeapSize)
	assert.Empty(t, out.AddressTableLookups)
}

// TestTransactionV1_BinaryRoundTrip asserts a full v1 transaction round-trips
// through the binary codec: the v1 envelope is the prefixed message followed by
// exactly numRequiredSignatures signatures, with no compact-u16 count.
func TestTransactionV1_BinaryRoundTrip(t *testing.T) {
	tx := &Transaction{
		Message:    *v1Message(t),
		Signatures: []Signature{{0xaa, 0xbb, 0xcc}},
	}

	encoded, err := tx.MarshalBinary()
	require.NoError(t, err)
	assert.Equal(t, messageVersionV1Prefix, encoded[0], "v1 transaction must start with the message's 0x81 prefix")

	got, err := TransactionFromBytes(encoded)
	require.NoError(t, err)
	assert.Equal(t, MessageVersionV1, got.Message.GetVersion())
	require.Len(t, got.Signatures, 1)
	assert.Equal(t, tx.Signatures[0], got.Signatures[0])
	assert.Equal(t, tx.Message.AccountKeys, got.Message.AccountKeys)
}

// TestTransactionV1_DispatchIsVersionAware guards the discriminator logic: a
// legacy transaction (compact-u16 signature count as its first byte) must not
// be misread as v1, and a v1 transaction must not be read through the
// legacy/v0 signature-count path.
func TestTransactionV1_DispatchIsVersionAware(t *testing.T) {
	legacy := &Transaction{
		Message: Message{
			version:         MessageVersionLegacy,
			AccountKeys:     PublicKeySlice{SystemProgramID},
			RecentBlockhash: Hash{1},
			Header:          MessageHeader{NumRequiredSignatures: 1},
		},
		Signatures: []Signature{{0x01}},
	}
	encoded, err := legacy.MarshalBinary()
	require.NoError(t, err)
	assert.Less(t, encoded[0], messageVersionPrefix, "legacy tx first byte is a small signature count")

	got, err := TransactionFromBytes(encoded)
	require.NoError(t, err)
	assert.Equal(t, MessageVersionLegacy, got.Message.GetVersion())
}
