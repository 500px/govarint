// TODO Would it make sense to first encode the field widths, ie: the
// output would be:
// <field width specifier length>, <field width>, <field>.

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
    may only be in the range of ints expressible in two bits (0..3)
    even though only at most one bit will be used to store the actual
    value.
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

	formatResult := make([]byte, 0, len(fields)*5)
	valueResult := make([]byte, 0, len(fields)*4)

	totalValueWidth := 0

	for i, fieldWidth := range fields {
		if fieldWidth == 0 {
			return []byte{}, fmt.Errorf("received invalid 0 field width")
		}

		leadingZeros := countLeadingZeros(values[i])
		valueWidth := 32 - leadingZeros

		// Zero value, nothing to add to value byte.
		if valueWidth == 0 {
			addBitsToSlice(&formatResult, 0, fieldWidth, &formatCurByte, &formatCurIndex, false)

			continue
		}

		if valueWidth > (1<<fieldWidth)-1 {
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
		if totalValueWidth < 8 {
			addBitsToSlice(&formatResult, uint32(b>>uint(8-totalValueWidth)), uint8(totalValueWidth), &formatCurByte, &formatCurIndex, false)
		} else {
			addBitsToSlice(&formatResult, uint32(b), uint8(8), &formatCurByte, &formatCurIndex, false)
			totalValueWidth -= 8
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

	fieldWidths := make([]uint8, 0, len(fields))
	values := make([]uint32, 0, len(fields))

	for _, formatWidth := range fields {
		curFieldWidth, err := popBitsFromSlice(&data, formatWidth, &curByte, &curIndex, false)
		if err != nil {
			return []uint32{}, err
		}
		fieldWidths = append(fieldWidths, uint8(curFieldWidth))
	}

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

		if dataBitIndex <= 24 {
			value |= uint32(curValue) << dataBitIndex
		} else {
			value |= uint32(curValue) >> (8 - (dataBitIndex % 8))
		}

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
	var remainingBits uint8
	var finalIndex uint8
	var shiftedValue uint64
	var shiftedMask uint64

	if skipFirstBit {
		if width <= 1 {
			return
		}
		remainingBits = width - 1
		finalIndex = (*curIndex + remainingBits) % 8
		// Unset leading 1.
		value &= (1 << remainingBits) - 1
		if *curIndex == 0 {
			*curIndex = 8
		}
		shiftedValue = uint64(value) << (40 - remainingBits - *curIndex)
		shiftedMask = ((1 << remainingBits) - 1) << (40 - remainingBits - *curIndex)
	} else {
		if width == 0 {
			return
		}
		remainingBits = width
		if *curIndex == 0 {
			*curIndex = 8
		}
		finalIndex = (*curIndex + remainingBits) % 8
		shiftedValue = uint64(value) << (40 - width - *curIndex)
		shiftedMask = ((1 << width) - 1) << (40 - width - *curIndex)
	}

	maskedValue := shiftedValue & shiftedMask

	// Handle leading partial byte.
	if *curIndex&0x7 != 0 {
		*curByte |= uint8(maskedValue >> 32)
		remainingBits -= 8 - *curIndex
		if remainingBits&0x80 != 0 {
			remainingBits = 0
			*curIndex = finalIndex
			return
		} else {
			*slice = append(*slice, *curByte)
		}
	}

	// Handle complete bytes.
	shiftAmount := uint8(24)
	for ; remainingBits >= 8; remainingBits -= 8 {
		*slice = append(*slice, uint8(maskedValue>>shiftAmount))
		shiftAmount -= 8
	}

	// Handle trailing partial byte.
	if remainingBits > 0 {
		*curByte = uint8(maskedValue >> shiftAmount)
	}

	*curIndex = finalIndex
}
