package govarint

import (
	"fmt"
)

// Return the number of leading zeros before the first set bit.
func countLeadingZeros(x uint32) int {
	if x == 0 {
		return 32
	}

	count := 0
	if (x & 0xffff0000) == 0 {
		count += 16
		x = x << 16
	}
	if (x & 0xff000000) == 0 {
		count += 8
		x = x << 8
	}
	if (x & 0xf0000000) == 0 {
		count += 4
		x = x << 4
	}
	if (x & 0xc0000000) == 0 {
		count += 2
		x = x << 2
	}
	if (x & 0x80000000) == 0 {
		count += 1
	}
	return count
}

/**
Encode the given values in the given varint format.

Args:
  fields: Ordered list of bit widths of fields. e.g.: 2 means two bits
    are allocated to specify the length of the value and so the value
    may only be in the range of ints expressible in two bits (0..3).
  values: Ordered list of values. If a value exceeds the allocated
    space an error will be returned.
*/
func Encode(fields []uint8, values []uint32) ([]byte, error) {
	if len(fields) != len(values) {
		return []byte{}, fmt.Errorf("mismatched field and value count, got %d fields and %d values", len(fields), len(values))
	}

	var formatCurByte uint8
	var formatCurIndex uint8
	var valueCurByte uint8
	var valueCurIndex uint8

	formatResult := []byte{}
	valueResult := []byte{}

	totalValueWidth := 0

	for i, fieldWidth := range fields {
		leadingZeros := countLeadingZeros(values[i])
		valueWidth := 32 - leadingZeros

		// Zero value, nothing to add to value byte.
		if valueWidth == 0 {
			addBitsToSlice(&formatResult, 0, fieldWidth, &formatCurByte, &formatCurIndex, false)

			continue
		}

		if uint64(values[i]) > uint64((1<<(1<<fieldWidth))-1) {
			return []byte{}, fmt.Errorf("value %d too large for field width %d", values[i], fieldWidth)
		}

		addBitsToSlice(&formatResult, uint32(valueWidth), fieldWidth, &formatCurByte, &formatCurIndex, false)

		addBitsToSlice(&valueResult, values[i], uint8(valueWidth), &valueCurByte, &valueCurIndex, true)

		totalValueWidth += valueWidth - 1
	}

	// Add trailing value bits.
	if valueCurIndex > 0 {
		addBitsToSlice(&valueResult, 0, 8-valueCurIndex, &valueCurByte, &valueCurIndex, false)
	}

	for _, b := range valueResult {
		curValueWidth := 8
		if totalValueWidth < 8 {
			curValueWidth = totalValueWidth
		}
		if curValueWidth == 8 {
			addBitsToSlice(&formatResult, uint32(b), uint8(curValueWidth), &formatCurByte, &formatCurIndex, false)
			totalValueWidth -= 8
		} else {
			addBitsToSlice(&formatResult, uint32(b>>uint(8-curValueWidth)), uint8(curValueWidth), &formatCurByte, &formatCurIndex, false)
		}
	}

	// Add trailing format bits.
	if formatCurIndex > 0 {
		addBitsToSlice(&formatResult, 0, 8-formatCurIndex, &formatCurByte, &formatCurIndex, false)
	}

	return formatResult, nil
}

func Decode(fields []uint8, data []byte) ([]uint32, error) {
	var curIndex uint8
	curByte := data[0]
	data = data[1:len(data)]

	fieldWidths := []uint8{}

	for _, formatWidth := range fields {
		curFieldWidth, err := popBitsFromSlice(&data, formatWidth, &curByte, &curIndex, false)
		if err != nil {
			return []uint32{}, err
		}
		fieldWidths = append(fieldWidths, uint8(curFieldWidth))
	}

	values := []uint32{}

	for _, width := range fieldWidths {
		curValue, err := popBitsFromSlice(&data, width, &curByte, &curIndex, true)
		if err != nil {
			return []uint32{}, err
		}

		values = append(values, curValue)
	}

	return values, nil
}

func popBitsFromSlice(slice *[]byte, width uint8, curByte *uint8, curIndex *uint8, addFirstBit bool) (uint32, error) {
	if width == 0 {
		return 0, nil
	}

	skipCount := uint8(0)
	if addFirstBit {
		skipCount = 1
	}

	// We only need to read from the current byte.
	if width+*curIndex-skipCount <= 8 {
		mask := uint8((1 << (width - skipCount)) - 1)
		mask <<= 8 - (width - skipCount) - *curIndex

		value := uint32((*curByte & mask) >> (8 - (width - skipCount) - *curIndex))

		if addFirstBit {
			*curIndex += width - 1
			value |= 1 << (width - 1)
		} else {
			*curIndex += width
		}

		if *curIndex < 8 {
			return value, nil
		}

		*curIndex = 0
		if len(*slice) != 0 {
			*curByte, *slice = (*slice)[0], (*slice)[1:len(*slice)]
		}

		return value, nil
	}

	mask := uint64((1 << width) - 1)
	mask <<= 40 - width - *curIndex
	// Effective mask should now be entirely in its bottom five bytes.

	var value uint32
	readByteIndex := uint8(4)
	dataBitIndex := uint8(24)

	var finalIndex uint8
	var remainingWidth uint8
	if addFirstBit {
		dataBitIndex--
		finalIndex = (*curIndex + width - 1) % 8
		remainingWidth = width - 1
	} else {
		finalIndex = (*curIndex + width) % 8
		remainingWidth = width
	}

	for ; remainingWidth > 0; readByteIndex-- {
		curMask := uint8(mask >> (readByteIndex * 8))
		curValue := uint8((*curByte & curMask) << *curIndex)

		value |= uint32(curValue) << dataBitIndex

		advancedWidth := 8 - *curIndex
		if remainingWidth < advancedWidth {
			advancedWidth = remainingWidth
		}

		consumeByte := *curIndex+advancedWidth == 8

		*curIndex = (*curIndex + advancedWidth) % 8

		if remainingWidth > advancedWidth {
			remainingWidth -= advancedWidth
		} else {
			remainingWidth = 0
		}

		dataBitIndex -= advancedWidth

		if remainingWidth != 0 {
			if len(*slice) == 0 {
				return 0, fmt.Errorf("ran out of data before end of value, expected additional %d bits of data", remainingWidth)
			}
		}

		if len(*slice) > 0 && consumeByte {
			*curByte, *slice = (*slice)[0], (*slice)[1:len(*slice)]
		}
	}

	value >>= 32 - width

	if addFirstBit {
		value |= 1 << (width - 1)
	}

	*curIndex = finalIndex

	return value, nil
}

func addBitsToSlice(slice *[]byte, value uint32, width uint8, curByte *uint8, curIndex *uint8, skipFirstBit bool) {
	if skipFirstBit {
		width--
	}

	if width == 0 {
		return
	}

	// Shift value to high bits, stripping the first bit if skipFirstBit
	// since we know it must be set (no need to store).
	var v uint64 = (uint64(value) << uint(64-width-*curIndex))

	remainingBits := width

	start := uint(56)

	// Handle first partial byte
	if *curIndex > 0 {
		*curByte |= uint8((v & ^(0xff << (64 - *curIndex))) >> start)
		start -= 8

		completedWidth := 8 - *curIndex
		if width < completedWidth {
			completedWidth = width
		}

		if width+*curIndex >= 8 {
			*slice = append(*slice, *curByte)
			*curByte = 0
			*curIndex = 0
		} else {
			*curIndex += completedWidth
		}

		if completedWidth == width {
			return
		}

		remainingBits -= completedWidth
	}

	for i := start; remainingBits > 0; i -= 8 {
		*curIndex = remainingBits % 8

		*curByte = uint8(v >> i)
		if remainingBits < 8 {
			break
		}
		*slice = append(*slice, *curByte)
		remainingBits -= 8
	}
}
