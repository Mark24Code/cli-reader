package mobibook

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

const pdbHeaderLen = 78

type pdbRecord struct {
	Offset int
	Size   int
	Flags  uint8
	ID     uint32
}

type palmDB struct {
	Name    string
	Type    string
	Creator string
	Records []pdbRecord

	r    io.ReaderAt
	size int64
}

func parsePalmDB(data []byte) (*palmDB, error) {
	return parsePalmDBReader(bytes.NewReader(data), int64(len(data)))
}

func parsePalmDBReader(r io.ReaderAt, size int64) (*palmDB, error) {
	if size < pdbHeaderLen {
		return nil, fmt.Errorf("文件过小，不是有效的 MOBI/PalmDB")
	}
	header := make([]byte, pdbHeaderLen)
	if _, err := r.ReadAt(header, 0); err != nil {
		return nil, err
	}
	name := strings.TrimRight(string(header[:32]), "\x00 ")
	typ := string(header[60:64])
	creator := string(header[64:68])
	if !(typ == "BOOK" && creator == "MOBI") && !(typ == "TEXt" && creator == "REAd") {
		return nil, fmt.Errorf("不是 MOBI/PalmDB 文件")
	}
	n := int(binary.BigEndian.Uint16(header[76:78]))
	if n <= 0 {
		return nil, fmt.Errorf("PalmDB 没有记录")
	}
	tableEnd := pdbHeaderLen + 8*n
	if int64(tableEnd) > size {
		return nil, fmt.Errorf("PalmDB 记录表越界")
	}
	table := make([]byte, 8*n)
	if _, err := r.ReadAt(table, pdbHeaderLen); err != nil {
		return nil, err
	}
	offsets := make([]int, n)
	for i := 0; i < n; i++ {
		offsets[i] = int(binary.BigEndian.Uint32(table[i*8:]))
		if offsets[i] < tableEnd || int64(offsets[i]) > size {
			return nil, fmt.Errorf("PalmDB 记录偏移无效")
		}
		if i > 0 && offsets[i] < offsets[i-1] {
			return nil, fmt.Errorf("PalmDB 记录偏移未排序")
		}
	}
	records := make([]pdbRecord, n)
	for i := 0; i < n; i++ {
		start := offsets[i]
		end := int(size)
		if i+1 < n {
			end = offsets[i+1]
		}
		hdr := i * 8
		records[i] = pdbRecord{
			Offset: start,
			Size:   end - start,
			Flags:  table[hdr+4],
			ID:     uint32(table[hdr+5])<<16 | uint32(table[hdr+6])<<8 | uint32(table[hdr+7]),
		}
	}
	return &palmDB{
		Name:    name,
		Type:    typ,
		Creator: creator,
		Records: records,
		r:       r,
		size:    size,
	}, nil
}

func (db *palmDB) recordData(i int) ([]byte, error) {
	if i < 0 || i >= len(db.Records) {
		return nil, fmt.Errorf("PalmDB 记录索引无效")
	}
	rec := db.Records[i]
	data := make([]byte, rec.Size)
	if rec.Size == 0 {
		return data, nil
	}
	if _, err := db.r.ReadAt(data, int64(rec.Offset)); err != nil && err != io.EOF {
		return nil, err
	}
	return data, nil
}
