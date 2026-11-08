package mobibook

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"cli-reader/internal/book"
	"cli-reader/internal/textdecode"
)

const (
	mobiHeaderBase  = 16
	exthFlagPresent = 0x40
)

type palmDocHeader struct {
	Compression     uint16
	TextLength      uint32
	TextRecordCount uint16
	RecordSize      uint16
	Encryption      uint16
}

func parsePalmDocHeader(rec0 []byte) (palmDocHeader, error) {
	if len(rec0) < 16 {
		return palmDocHeader{}, fmt.Errorf("PalmDOC 头过短")
	}
	return palmDocHeader{
		Compression:     binary.BigEndian.Uint16(rec0[0:2]),
		TextLength:      binary.BigEndian.Uint32(rec0[4:8]),
		TextRecordCount: binary.BigEndian.Uint16(rec0[8:10]),
		RecordSize:      binary.BigEndian.Uint16(rec0[10:12]),
		Encryption:      binary.BigEndian.Uint16(rec0[12:14]),
	}, nil
}

type mobiHeader struct {
	HeaderLen           uint32
	MobiType            uint32
	TextEncoding        uint32
	FileVersion         uint32
	FirstNonBookIndex   uint32
	FullNameOffset      uint32
	FullNameLength      uint32
	FirstImageIndex     uint32
	HuffmanRecordOffset uint32
	HuffmanRecordCount  uint32
	EXTHFlags           uint32
	DRMOffset           uint32
	DRMCount            uint32
	DRMSize             uint32
	ExtraRecordData     uint16
	INDXRecordOffset    uint32
}

func parseMOBIHeader(rec0 []byte) (*mobiHeader, error) {
	if len(rec0) < mobiHeaderBase+4 || string(rec0[mobiHeaderBase:mobiHeaderBase+4]) != "MOBI" {
		return nil, nil // PalmDOC-only book
	}
	if len(rec0) < mobiHeaderBase+0xE8 {
		return nil, fmt.Errorf("MOBI 头过短")
	}
	mh := &mobiHeader{
		HeaderLen:           binary.BigEndian.Uint32(rec0[mobiHeaderBase+4:]),
		MobiType:            binary.BigEndian.Uint32(rec0[mobiHeaderBase+8:]),
		TextEncoding:        binary.BigEndian.Uint32(rec0[mobiHeaderBase+12:]),
		FileVersion:         binary.BigEndian.Uint32(rec0[mobiHeaderBase+20:]),
		FirstNonBookIndex:   binary.BigEndian.Uint32(rec0[mobiHeaderBase+64:]),
		FullNameOffset:      binary.BigEndian.Uint32(rec0[mobiHeaderBase+68:]),
		FullNameLength:      binary.BigEndian.Uint32(rec0[mobiHeaderBase+72:]),
		FirstImageIndex:     binary.BigEndian.Uint32(rec0[mobiHeaderBase+92:]),
		HuffmanRecordOffset: binary.BigEndian.Uint32(rec0[mobiHeaderBase+96:]),
		HuffmanRecordCount:  binary.BigEndian.Uint32(rec0[mobiHeaderBase+100:]),
		EXTHFlags:           binary.BigEndian.Uint32(rec0[mobiHeaderBase+112:]),
		DRMOffset:           binary.BigEndian.Uint32(rec0[mobiHeaderBase+148:]),
		DRMCount:            binary.BigEndian.Uint32(rec0[mobiHeaderBase+152:]),
		DRMSize:             binary.BigEndian.Uint32(rec0[mobiHeaderBase+156:]),
		ExtraRecordData:     binary.BigEndian.Uint16(rec0[mobiHeaderBase+226:]),
		INDXRecordOffset:    binary.BigEndian.Uint32(rec0[mobiHeaderBase+228:]),
	}
	if mh.HeaderLen > uint32(len(rec0)) {
		return nil, fmt.Errorf("MOBI 头长度无效")
	}
	return mh, nil
}

type exthData struct {
	Title       string
	Authors     []string
	Language    string
	ASIN        string
	CoverOffset int
	ThumbOffset int
	KF8Boundary bool
	FixedLayout bool
	Rental      string
}

func parseEXTH(rec0 []byte, mh *mobiHeader, textEncoding uint32) (*exthData, error) {
	if mh == nil || mh.EXTHFlags&exthFlagPresent == 0 {
		return &exthData{}, nil
	}
	off := mobiHeaderBase + int(mh.HeaderLen)
	if off+12 > len(rec0) || string(rec0[off:off+4]) != "EXTH" {
		return &exthData{}, nil
	}
	length := int(binary.BigEndian.Uint32(rec0[off+4:]))
	count := int(binary.BigEndian.Uint32(rec0[off+8:]))
	if length < 12 || off+length > len(rec0) {
		return nil, fmt.Errorf("EXTH 长度无效")
	}
	out := &exthData{}
	p := off + 12
	for i := 0; i < count && p+8 <= off+length; i++ {
		typ := binary.BigEndian.Uint32(rec0[p:])
		size := int(binary.BigEndian.Uint32(rec0[p+4:]))
		if size < 8 || p+size > off+length {
			return nil, fmt.Errorf("EXTH 条目越界")
		}
		data := rec0[p+8 : p+size]
		switch typ {
		case 503, 99:
			if out.Title == "" {
				out.Title = decodeMOBIText(data, textEncoding)
			}
		case 100:
			if s := decodeMOBIText(data, textEncoding); s != "" {
				out.Authors = append(out.Authors, s)
			}
		case 524:
			out.Language = decodeMOBIText(data, textEncoding)
		case 113:
			out.ASIN = decodeMOBIText(data, textEncoding)
		case 201:
			out.CoverOffset = int(binary.BigEndian.Uint32(data))
		case 202:
			out.ThumbOffset = int(binary.BigEndian.Uint32(data))
		case 121:
			out.KF8Boundary = true
		case 122:
			out.FixedLayout = valueNonZero(data)
		case 406:
			out.Rental = decodeMOBIText(data, textEncoding)
		}
		p += size
	}
	return out, nil
}

func valueNonZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return true
		}
	}
	return false
}

func decodeMOBIText(data []byte, encoding uint32) string {
	switch encoding {
	case 65001:
		if utf8.Valid(data) {
			return book.CollapseSpace(string(data))
		}
	case 1252:
		if decoded, err := charmap.Windows1252.NewDecoder().Bytes(data); err == nil {
			return book.CollapseSpace(string(decoded))
		}
	}
	if utf8.Valid(data) {
		return book.CollapseSpace(string(data))
	}
	return book.CollapseSpace(textdecode.Decode(data).Text)
}

func (mh *mobiHeader) fullName(rec0 []byte) string {
	if mh == nil {
		return ""
	}
	start := int(mh.FullNameOffset)
	end := start + int(mh.FullNameLength)
	if start < 0 || end > len(rec0) || start >= end {
		return ""
	}
	return decodeMOBIText(rec0[start:end], mh.TextEncoding)
}

func (mh *mobiHeader) hasDRM() bool {
	if mh == nil {
		return false
	}
	if mh.DRMOffset == 0xFFFFFFFF || mh.DRMCount == 0xFFFFFFFF {
		return false
	}
	return mh.DRMSize > 0 || mh.DRMCount > 0
}

func (mh *mobiHeader) isKF8() bool {
	return mh != nil && (mh.FileVersion >= 8 || mh.HeaderLen >= 264)
}

func (mh *mobiHeader) encodingLabel() string {
	if mh == nil {
		return "windows-1252"
	}
	switch mh.TextEncoding {
	case 65001:
		return "utf-8"
	case 1252:
		return "windows-1252"
	}
	return "unknown"
}

var _ = strings.TrimSpace
