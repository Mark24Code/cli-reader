package mobibook

import "fmt"

// trimTrailingData removes MOBI extra record data using the extra-data flags.
// The algorithm mirrors KindleUnpack: indexed entries are stripped newest
// first, then the multibyte overlap byte (if present) removes 1-4 bytes.
func trimTrailingData(data []byte, flags uint16) []byte {
	if flags == 0 || len(data) == 0 {
		return data
	}
	multibyte := flags&1 != 0
	trailers := 0
	f := flags >> 1
	for f > 0 {
		if f&1 != 0 {
			trailers++
		}
		f >>= 1
	}
	for i := 0; i < trailers; i++ {
		size := sizeOfTrailingEntry(data)
		if size <= 0 || size > len(data) {
			return data
		}
		data = data[:len(data)-size]
	}
	if multibyte && len(data) > 0 {
		n := int(data[len(data)-1]&0x03) + 1
		if n <= len(data) {
			data = data[:len(data)-n]
		}
	}
	return data
}

func sizeOfTrailingEntry(data []byte) int {
	start := len(data) - 4
	if start < 0 {
		start = 0
	}
	num := 0
	for _, v := range data[start:] {
		if v&0x80 != 0 {
			num = 0
		}
		num = (num << 7) | int(v&0x7f)
	}
	return num
}

// palmDocDecompress expands PalmDOC LZ77 data.
func palmDocDecompress(data []byte, limit int) ([]byte, error) {
	out := make([]byte, 0, len(data)*2)
	for p := 0; p < len(data); {
		c := data[p]
		p++
		switch {
		case c >= 1 && c <= 8:
			n := int(c)
			if p+n > len(data) {
				return nil, fmt.Errorf("PalmDOC 字面量越界")
			}
			out = append(out, data[p:p+n]...)
			p += n
		case c < 128:
			out = append(out, c)
		case c >= 192:
			out = append(out, ' ', c^0x80)
		default:
			if p >= len(data) {
				return nil, fmt.Errorf("PalmDOC 回溯字节缺失")
			}
			pair := int(c)<<8 | int(data[p])
			p++
			distance := (pair >> 3) & 0x07FF
			length := (pair & 7) + 3
			if distance <= 0 || distance > len(out) {
				return nil, fmt.Errorf("PalmDOC 回溯距离无效")
			}
			start := len(out) - distance
			for j := 0; j < length; j++ {
				out = append(out, out[start+j])
			}
		}
		if limit > 0 && len(out) > limit {
			return nil, fmt.Errorf("PalmDOC 解压结果超过限制")
		}
	}
	return out, nil
}
