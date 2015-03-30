package govarint

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/ugorji/go/codec"
)

type Activity struct {
	Version    uint8  `codec:version`
	Action     uint8  `codec:action`
	ActorType  uint8  `codec:actorType`
	ActorID    uint32 `codec:actorID`
	ObjectType uint8  `codec:objectType`
	ObjectID   uint32 `codec:objectID`
}

type (
	leadingZeroTestCase struct {
		value uint32
		count int
	}
)

type encodeTestCase struct {
	fields []uint8
	values []uint32
	result []byte
}

type decodeTestCase struct {
	fields []uint8
	data   []byte
	values []uint32
}

type addBitsTestCase struct {
	slice         []byte
	value         uint32
	width         uint8
	curByte       uint8
	curIndex      uint8
	skipFirstBit  bool
	expectedSlice []byte
	expectedByte  uint8
	expectedIndex uint8
}

type popBitsTestCase struct {
	slice         []byte
	width         uint8
	curByte       uint8
	curIndex      uint8
	addFirstBit   bool
	expectedValue uint32
	expectedError error
	expectedSlice []byte
	expectedByte  uint8
	expectedIndex uint8
}

type roundTripTestCase struct {
	fields []uint8
	values []uint32
}

var (
	roundTripTests = []roundTripTestCase{
		{[]uint8{1}, []uint32{0}},
		{[]uint8{1}, []uint32{1}},
		{[]uint8{3}, []uint32{8}},

		{[]uint8{4, 3}, []uint32{8, 4}},

		{[]uint8{4, 5}, []uint32{8, 12345}},

		{[]uint8{3, 6}, []uint32{1, 4294967295}},

		{[]uint8{4, 5}, []uint32{1, 12345}},

		// Action type, actor type, actor ID, object type, object ID.
		{[]uint8{3, 3, 6, 3, 6}, []uint32{1, 5, 1128411, 2, 123456789}},

		{[]uint8{3, 3}, []uint32{0, 5}},
	}

	leadingZeroTests = []leadingZeroTestCase{
		{0, 32},
		{1, 31},
		{2, 30},
		{3, 30},
		{4, 29},
		{5, 29},
		{6, 29},
		{7, 29},
		{8, 28},
		{31, 27},
		{32, 26},
		{63, 26},
		{64, 25},
		{65, 25},
		{1<<32 - 1, 0},
	}

	encodeTests = []encodeTestCase{
		// Single zero value takes up only the space for the field
		// specifier.
		{[]uint8{1}, []uint32{0}, []byte{0}},
		{[]uint8{8}, []uint32{0}, []byte{0}},
		{[]uint8{9}, []uint32{0}, []byte{0, 0}},

		// A one-value takes up only the space for the field specifier.
		{[]uint8{1}, []uint32{1}, []byte{0x80}},
		{[]uint8{2}, []uint32{1}, []byte{0x40}},

		// Single non-zero value takes up the space for the field
		// specifier plus len(value) - 1.
		{[]uint8{2}, []uint32{3}, []byte{0xa0}},

		{[]uint8{2, 1}, []uint32{3, 0}, []byte{0x90}},
		{[]uint8{2, 1}, []uint32{3, 1}, []byte{0xb0}},

		{[]uint8{2, 1}, []uint32{0, 0}, []byte{0}},
		{[]uint8{2, 1}, []uint32{0, 1}, []byte{0x20}},

		{[]uint8{2, 1}, []uint32{0, 1}, []byte{0x20}},

		{[]uint8{3, 1}, []uint32{0, 1}, []byte{0x10}},
		{[]uint8{3, 1}, []uint32{1, 1}, []byte{0x30}},
		{[]uint8{3, 1}, []uint32{2, 1}, []byte{0x50}},
		{[]uint8{3, 1}, []uint32{3, 1}, []byte{0x58}},
		{[]uint8{3, 1}, []uint32{4, 1}, []byte{0x70}},
		{[]uint8{3, 1}, []uint32{5, 1}, []byte{0x74}},
		{[]uint8{3, 1}, []uint32{6, 1}, []byte{0x78}},
		{[]uint8{3, 1}, []uint32{7, 1}, []byte{0x7c}},

		{[]uint8{3, 1}, []uint32{8, 1}, []byte{0x90}},
		{[]uint8{3, 1}, []uint32{9, 1}, []byte{0x92}},
		{[]uint8{3, 1}, []uint32{10, 1}, []byte{0x94}},
		{[]uint8{3, 1}, []uint32{11, 1}, []byte{0x96}},

		{[]uint8{6, 1}, []uint32{0xffffffff, 1}, []byte{0x83, 0xff, 0xff, 0xff, 0xfc}},

		{[]uint8{4, 5}, []uint32{1, 12345}, []byte{0x17, 0x40, 0xe4}},

		{[]uint8{4, 5}, []uint32{8, 12345}, []byte{0x47, 0x08, 0x1c, 0x80}},

		{[]uint8{3, 3}, []uint32{0, 5}, []byte{0x0d}},

		{[]uint8{6}, []uint32{0xb6369222}, []byte{0x81, 0xb1, 0xb4, 0x91, 0x10}},
	}

	decodeTests = []decodeTestCase{
		{[]uint8{1}, []byte{0}, []uint32{0}},

		{[]uint8{4, 5}, []byte{0x17, 0x40, 0xe4}, []uint32{1, 12345}},

		{[]uint8{4, 5}, []byte{0x47, 0x08, 0x1c, 0x80}, []uint32{8, 12345}},

		{[]uint8{3, 3}, []byte{0x0d}, []uint32{0, 5}},

		{[]uint8{6}, []byte{0x81, 0xb1, 0xb4, 0x91, 0x10}, []uint32{0xb6369222}},
	}

	addBitsTests = []addBitsTestCase{
		{[]byte{}, 0, 0, 0, 0, false, []byte{}, 0, 0},

		// Single bit field with bit set should be only a 1 for field
		// specifier and nothing for value.
		{[]byte{}, 1, 1, 0, 0, false, []byte{}, 0x80, 1},

		{[]byte{}, 1 << 31, 32, 0, 0, false, []byte{0x80, 0, 0, 0}, 0, 0},

		{[]byte{0}, 2, 2, 0, 0, false, []byte{0}, 0x80, 2},
		{[]byte{0}, 2, 2, 0x80, 2, false, []byte{0}, 0xa0, 4},

		{[]byte{}, 1 << 31, 32, 0, 0, true, []byte{0, 0, 0}, 0, 7},

		{[]byte{}, 0x0e, 5, 0x10, 4, false, []byte{0x17}, 0x00, 1},

		{[]byte{}, 32, 6, 0x00, 0, false, []byte{}, 0x80, 6},

		{[]byte{}, 0xb6369222, 32, 0x80, 6, true, []byte{0x81, 0xb1, 0xb4, 0x91}, 0x10, 5},
	}

	popBitsTests = []popBitsTestCase{
		{[]byte{}, 1, 0x00, 0, false, 0, nil, []byte{}, 0x00, 1},
		{[]byte{}, 1, 0x80, 0, false, 1, nil, []byte{}, 0x80, 1},

		{[]byte{}, 1, 0x00, 1, false, 0, nil, []byte{}, 0x00, 2},
		{[]byte{}, 1, 0x40, 1, false, 1, nil, []byte{}, 0x40, 2},

		{[]byte{}, 1, 0x00, 7, false, 0, nil, []byte{}, 0x00, 0},
		{[]byte{}, 1, 0x01, 7, false, 1, nil, []byte{}, 0x01, 0},

		{[]byte{0xff}, 1, 0x00, 7, false, 0, nil, []byte{}, 0xff, 0},
		{[]byte{0xff}, 1, 0x01, 7, false, 1, nil, []byte{}, 0xff, 0},

		{[]byte{0x12, 0x34}, 1, 0x00, 7, false, 0, nil, []byte{0x34}, 0x12, 0},
		{[]byte{0x12, 0x34}, 1, 0x01, 7, false, 1, nil, []byte{0x34}, 0x12, 0},

		// Byte aligned multi-byte values.
		{[]byte{0x34}, 16, 0x12, 0, false, 0x1234, nil, []byte{}, 0x34, 0},
		{[]byte{0x34, 0x56}, 16, 0x12, 0, false, 0x1234, nil, []byte{}, 0x56, 0},
		{[]byte{0x34, 0x56}, 24, 0x12, 0, false, 0x123456, nil, []byte{}, 0x56, 0},

		// Unaligned multi-byte values.
		{[]byte{0x24, 0x68}, 16, 0x00, 7, false, 0x1234, nil, []byte{}, 0x68, 7},
		{[]byte{0x1a, 0x00}, 16, 0x09, 1, false, 0x1234, nil, []byte{}, 0x00, 1},
		{[]byte{0x46, 0x80}, 16, 0xe2, 3, false, 0x1234, nil, []byte{}, 0x80, 3},

		// Unaligned short values (less than 8 bits) spanning bytes.
		{[]byte{0xff}, 2, 0xff, 7, false, 0x3, nil, []byte{}, 0xff, 1},

		// Tests adding the first bit.
		{[]byte{}, 1, 0x00, 0, true, 1, nil, []byte{}, 0x00, 0},
		{[]byte{}, 2, 0x00, 0, true, 2, nil, []byte{}, 0x00, 1},

		// Add bit across byte boundary.
		{[]byte{0x80}, 3, 0x01, 7, true, 7, nil, []byte{}, 0x80, 1},

		{[]byte{0x8d, 0x00}, 13, 0x00, 6, true, 0x1234, nil, []byte{}, 0x00, 2},

		{[]byte{0x40, 0xe4}, 4, 0x17, 0, false, 0x01, nil, []byte{0x40, 0xe4}, 0x17, 4},
		{[]byte{0x40, 0xe4}, 5, 0x17, 4, false, 0x0e, nil, []byte{0xe4}, 0x40, 1},
		{[]byte{0xe4}, 1, 0x40, 1, true, 0x01, nil, []byte{0xe4}, 0x40, 1},
		{[]byte{0xe4}, 14, 0x40, 1, true, 0x3039, nil, []byte{}, 0xe4, 6},

		{[]byte{0x08, 0x1c, 0x80}, 4, 0x47, 0, false, 4, nil, []byte{0x08, 0x1c, 0x80}, 0x47, 4},
		{[]byte{0x08, 0x1c, 0x80}, 5, 0x47, 4, false, 14, nil, []byte{0x1c, 0x80}, 0x08, 1},
		{[]byte{0x1c, 0x80}, 4, 0x08, 1, true, 8, nil, []byte{0x1c, 0x80}, 0x08, 4},

		{[]byte{}, 3, 0x0d, 0, false, 0, nil, []byte{}, 0x0d, 3},
		{[]byte{}, 3, 0x0d, 3, false, 3, nil, []byte{}, 0x0d, 6},
		{[]byte{}, 0, 0x0d, 6, true, 0, nil, []byte{}, 0x0d, 6},
		{[]byte{}, 3, 0x0d, 6, true, 5, nil, []byte{}, 0x0d, 0},

		{[]byte{0xb1, 0xb4, 0x91, 0x10}, 6, 0x81, 0, false, 32, nil, []byte{0xb1, 0xb4, 0x91, 0x10}, 0x81, 6},
		{[]byte{0xb1, 0xb4, 0x91, 0x10}, 32, 0x81, 6, true, 0xb6369222, nil, []byte{}, 0x10, 5},
	}
)

func TestRoundTrip(t *testing.T) {
	for _, tc := range roundTripTests {
		_, err := executeRoundTrip(tc)
		if err != nil {
			t.Errorf(err.Error())
		}
	}
}

func executeRoundTrip(tc roundTripTestCase) (uint, error) {
	data, err := Encode(tc.fields, tc.values)
	if err != nil {
		return 0, fmt.Errorf("Unexpected encode error \"%s\" for %v", err, tc)
	}

	size := uint(len(data))

	result, err := Decode(tc.fields, data)
	if err != nil {
		return 0, fmt.Errorf("Unexpected decode error \"%s\" for %v", err, tc)
	}

	if len(tc.values) != len(result) {
		return 0, fmt.Errorf("Value count not equal, expected %d, got %d for %v", len(tc.values), len(result), tc)
	}

	for i, expected := range tc.values {
		if expected != result[i] {
			return 0, fmt.Errorf("Incorrect value, expected 0x%08x, got 0x%08x for %v", expected, result[i], tc)
		}
	}

	return size, nil
}

func TestRandomRoundTrips(t *testing.T) {
	seed := time.Now().UnixNano()
	rand.Seed(seed)
	fmt.Printf("Random seed: %d\n", seed)

	allCases := []roundTripTestCase{}

	for testCount := 0; testCount < 1000000; testCount++ {
		valueCount := int(rand.Int31n(30)) + 1

		values := []uint32{}
		fields := []uint8{}
		for i := 0; i < valueCount; i++ {
			curValue := uint32(rand.Int63() & ((1 << uint(rand.Int31n(33))) - 1))
			values = append(values, curValue)

			valueLength := int32(32 - countLeadingZeros(curValue))
			fieldWidth := rand.Int31n(int32(32-valueLength+1)) + valueLength
			fieldWidth = int32(32 - countLeadingZeros(uint32(fieldWidth)))
			if fieldWidth == 0 {
				fieldWidth = 1
			}
			fields = append(fields, uint8(fieldWidth))
		}

		allCases = append(allCases, roundTripTestCase{fields, values})
	}

	if err := compareRoundTrips(allCases); err != nil {
		t.Error(err.Error())
	}
}

func compareRoundTrips(allCases []roundTripTestCase) error {
	var totalCustomSize uint
	var totalStandardSize uint
	var totalCustomTime int64
	var totalStandardTime int64

	for _, tc := range allCases {
		start := time.Now().UnixNano()
		customSize, err := executeRoundTrip(tc)
		totalCustomTime += time.Now().UnixNano() - start
		if err != nil {
			return err
		}

		start = time.Now().UnixNano()
		standardSize := encodeStandardVarint(tc)
		totalStandardTime += time.Now().UnixNano() - start

		totalCustomSize += customSize
		totalStandardSize += standardSize
	}

	fmt.Printf("Custom varint would in total have used: %d bytes\n", totalCustomSize)
	fmt.Printf("Standard library varint would in total have used: %d bytes\n", totalStandardSize)
	fmt.Printf("Custom varint total was %02f %% of standard library varint.\n", float32(totalCustomSize)/float32(totalStandardSize))

	fmt.Printf("Custom varint took %s, standard library took %s.\n", time.Duration(totalCustomTime), time.Duration(totalStandardTime))

	return nil
}

func TestFoRealz(t *testing.T) {
	seed := time.Now().UnixNano()
	rand.Seed(seed)
	fmt.Printf("Random seed: %d\n", seed)

	allCases := []roundTripTestCase{}

	for i := 0; i < 1000000; i++ {
		// Version: 0-127
		// Action type: 0-7
		// Actor type: 0-7
		// Actor ID: 0-4294967296
		// Object type: 0-7
		// Object ID: 0-4294967296
		tc := roundTripTestCase{fields: []uint8{4, 2, 2, 6, 2, 6}}

		values := []uint32{}

		values = append(values, uint32(rand.Int31n(16)))
		values = append(values, uint32(rand.Int31n(8)))
		values = append(values, uint32(rand.Int31n(7)))
		values = append(values, uint32(rand.Int31n(100000000)))
		values = append(values, uint32(rand.Int31n(7)))
		values = append(values, uint32(rand.Int31n(400000000)*10))

		tc.values = values

		allCases = append(allCases, tc)
	}

	if err := compareRoundTrips(allCases); err != nil {
		t.Error(err.Error())
	}

	msgPackTest(t, allCases)
}

func msgPackTest(t *testing.T, allCases []roundTripTestCase) {
	var totalSize uint
	var totalTime int64

	for _, tc := range allCases {
		a := Activity{
			Version:    uint8(tc.values[0]),
			Action:     uint8(tc.values[1]),
			ActorType:  uint8(tc.values[2]),
			ActorID:    tc.values[3],
			ObjectType: uint8(tc.values[4]),
			ObjectID:   tc.values[5],
		}

		start := time.Now().UnixNano()
		mh := &codec.MsgpackHandle{RawToString: true}
		bytes := []byte{}
		enc := codec.NewEncoderBytes(&bytes, mh)
		err := enc.Encode(&a)
		if err != nil {
			t.Error(err.Error())
		}
		totalSize += uint(len(bytes))

		newA := Activity{}
		dec := codec.NewDecoderBytes(bytes, mh)
		err = dec.Decode(&newA)
		totalTime += time.Now().UnixNano() - start
		if err != nil {
			t.Error(err.Error())
		}

		if a.Version != newA.Version {
			t.Error("Mismatched version\n")
		}
		if a.Action != newA.Action {
			t.Error("Mismatched version\n")
		}
		if a.ActorType != newA.ActorType {
			t.Error("Mismatched actor type\n")
		}
		if a.ActorID != newA.ActorID {
			t.Error("Mismatched actor ID\n")
		}
		if a.ObjectType != newA.ObjectType {
			t.Error("Mismatched object type\n")
		}
		if a.ObjectID != newA.ObjectID {
			t.Error("Mismatched object ID\n")
		}
	}

	fmt.Printf("Msgpack would in total have used: %d bytes\n", totalSize)
	fmt.Printf("Msgpack took %s.\n", time.Duration(totalTime))
}

func encodeStandardVarint(tc roundTripTestCase) uint {
	buf := make([]byte, 512)

	i := 0
	for _, v := range tc.values {
		written := binary.PutUvarint(buf[i:], uint64(v))
		i += written
	}

	i = 0
	for _, v := range tc.values {
		actual, read := binary.Uvarint(buf[i:])
		if uint32(actual) != v {
			fmt.Printf("Expected 0x%x, got 0x%x\n", v, actual)
		}
		i += read
	}

	return uint(i)
}

func TestCountLeadingZeros(t *testing.T) {
	for _, tc := range leadingZeroTests {
		count := countLeadingZeros(tc.value)
		if tc.count != count {
			t.Errorf("Expected %d, got %d for %d", tc.count, count, tc.value)
			continue
		}
	}
}

func TestDecode(t *testing.T) {
	for _, tc := range decodeTests {
		tcs := fmt.Sprintf("%v", tc)

		result, err := Decode(tc.fields, tc.data)
		if err != nil {
			t.Errorf("Unexpected decode error \"%s\" for %v", err, tcs)
			continue
		}

		if len(tc.values) != len(result) {
			t.Errorf("Value count not equal, expected %d, got %d for %v", len(tc.values), len(result), tcs)
			continue
		}

		for i, expected := range tc.values {
			if expected != result[i] {
				t.Errorf("Incorrect value, expected 0x%08x, got 0x%08x for %v", expected, result[i], tcs)
			}
		}
	}
}

func TestEncode(t *testing.T) {
	for _, tc := range encodeTests {
		result, err := Encode(tc.fields, tc.values)
		if err != nil {
			t.Errorf("Unexpected error: %s", err)
			continue
		}

		if !bytes.Equal(result, tc.result) {
			t.Errorf("Expected 0x%x, got 0x%x for %v", tc.result, result, tc)
			continue
		}
	}
}

func TestInvalidEncode(t *testing.T) {
	_, err := Encode([]uint8{1}, []uint32{4})

	if err == nil {
		t.Errorf("Did not receive expected error")
		return
	}

	expected := "value 4 too large for field width 1"
	if err.Error() != expected {
		t.Errorf("Expected error \"%s\", got: %s", expected, err)
		return
	}
}

func TestAddBitsToSlice(t *testing.T) {
	for _, tc := range addBitsTests {
		tcs := fmt.Sprintf("%v", tc)
		addBitsToSlice(&tc.slice, tc.value, tc.width, &tc.curByte, &tc.curIndex, tc.skipFirstBit)

		if !bytes.Equal(tc.slice, tc.expectedSlice) {
			t.Errorf("Expected 0x%x, got 0x%x for %v", tc.expectedSlice, tc.slice, tcs)
			continue
		}
		if tc.curByte != tc.expectedByte {
			t.Errorf("Expected current byte 0x%x, got 0x%x for %v", tc.expectedByte, tc.curByte, tcs)
			continue
		}
		if tc.curIndex != tc.expectedIndex {
			t.Errorf("Expected current index %d, got %d for %v", tc.expectedIndex, tc.curIndex, tcs)
			continue
		}
	}
}

func TestPopBitsFromSlice(t *testing.T) {
	for _, tc := range popBitsTests {
		tcs := fmt.Sprintf("%v", tc)

		value, err := popBitsFromSlice(&tc.slice, tc.width, &tc.curByte, &tc.curIndex, tc.addFirstBit)

		if err != nil && tc.expectedError == nil {
			t.Errorf("Unexpected error \"%s\" for %v", err, tcs)
		} else if err == nil && tc.expectedError != nil {
			t.Errorf("Expected error \"%s\", received no error for %v", tc.expectedError, tcs)
		} else if err != nil && tc.expectedError != nil && err.Error() != tc.expectedError.Error() {
			t.Errorf("Expected error \"%s\", received error \"%s\" for %v", tc.expectedError, err, tcs)
		}

		if value != tc.expectedValue {
			t.Errorf("Expected 0x%x, got 0x%x for %v", tc.expectedValue, value, tcs)
		}
		if !bytes.Equal(tc.slice, tc.expectedSlice) {
			t.Errorf("Expected 0x%x, got 0x%x for %v", tc.expectedSlice, tc.slice, tcs)
		}
		if tc.curByte != tc.expectedByte {
			t.Errorf("Expected current byte 0x%x, got 0x%x for %v", tc.expectedByte, tc.curByte, tcs)
		}
		if tc.curIndex != tc.expectedIndex {
			t.Errorf("Expected current index %d, got %d for %v", tc.expectedIndex, tc.curIndex, tcs)
		}
	}
}
