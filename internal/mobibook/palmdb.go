package mobibook

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const pdbHeaderLen = 78

type pdbRecord struct {
	Offset int
	Size   int
	Flags  uint8
	ID     uint32
	Data   []byte
}

type palmDB struct {
	Name    string
	Type    string
	Creator string
	Records []pdbRecord
}

func parsePalmDB(data []byte) (*palmDB, error) {
	if len(data) < pdbHeaderLen {
		return nil, fmt.Errorf("文件过小，不是有效的 MOBI/PalmDB")
	}
	name := strings.TrimRight(string(data[:32]), "\x00 ")
	typ := string(data[60:64])
	creator := string(data[64:68])
	if !(typ == "BOOK" && creator == "MOBI") && !(typ == "TEXt" && creator == "REAd") {
		return nil, fmt.Errorf("不是 MOBI/PalmDB 文件")
	}
	n := int(binary.BigEndian.Uint16(data[76:78]))
	if n <= 0 {
		return nil, fmt.Errorf("PalmDB 没有记录")
	}
	tableEnd := pdbHeaderLen + 8*n
	if tableEnd > len(data) {
		return nil, fmt.Errorf("PalmDB 记录表越界")
	}
	offsets := make([]int, n)
	for i := 0; i < n; i++ {
		offsets[i] = int(binary.BigEndian.Uint32(data[pdbHeaderLen+i*8:]))
		if offsets[i] < tableEnd || offsets[i] > len(data) {
			return nil, fmt.Errorf("PalmDB 记录偏移无效")
		}
		if i > 0 && offsets[i] < offsets[i-1] {
			return nil, fmt.Errorf("PalmDB 记录偏移未排序")
		}
	}
	records := make([]pdbRecord, n)
	for i := 0; i < n; i++ {
		start := offsets[i]
		end := len(data)
		if i+1 < n {
			end = offsets[i+1]
		}
		hdr := pdbHeaderLen + i*8
		records[i] = pdbRecord{
			Offset: start,
			Size:   end - start,
			Flags:  data[hdr+4],
			ID:     uint32(data[hdr+5])<<16 | uint32(data[hdr+6])<<8 | uint32(data[hdr+7]),
			Data:   data[start:end],
		}
	}
	return &palmDB{Name: name, Type: typ, Creator: creator, Records: records}, nil
}
