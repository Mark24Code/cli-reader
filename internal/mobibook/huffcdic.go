package mobibook

import (
	"encoding/binary"
	"fmt"
)

type huffDict1Entry struct {
	codelen int
	term    bool
	maxcode uint64
}

type huffPhrase struct {
	data    []byte
	decoded []byte
	done    bool
	literal bool
}

type huffReader struct {
	dict1      [256]huffDict1Entry
	mincode    [33]uint64
	maxcode    [33]uint64
	dictionary []huffPhrase
}

func newHuffReader(huff []byte, cdics [][]byte) (*huffReader, error) {
	h := &huffReader{}
	if err := h.loadHuff(huff); err != nil {
		return nil, err
	}
	for _, cdic := range cdics {
		if err := h.loadCDIC(cdic); err != nil {
			return nil, err
		}
	}
	if len(h.dictionary) == 0 {
		return nil, fmt.Errorf("HUFF/CDIC 字典为空")
	}
	return h, nil
}

func (h *huffReader) loadHuff(huff []byte) error {
	if len(huff) < 16 || string(huff[:4]) != "HUFF" {
		return fmt.Errorf("HUFF 记录无效")
	}
	off1 := int(binary.BigEndian.Uint32(huff[8:12]))
	off2 := int(binary.BigEndian.Uint32(huff[12:16]))
	if off1 < 0 || off1+256*4 > len(huff) {
		return fmt.Errorf("HUFF 字典 1 越界")
	}
	if off2 < 0 || off2+64*4 > len(huff) {
		return fmt.Errorf("HUFF 字典 2 越界")
	}
	for i := 0; i < 256; i++ {
		v := binary.BigEndian.Uint32(huff[off1+i*4:])
		codelen := int(v & 0x1F)
		term := v&0x80 != 0
		max := uint64(v >> 8)
		if codelen == 0 || codelen > 32 {
			return fmt.Errorf("HUFF 码长无效")
		}
		full := max
		if codelen < 32 {
			full = ((max + 1) << (32 - codelen)) - 1
		}
		h.dict1[i] = huffDict1Entry{codelen: codelen, term: term, maxcode: full}
	}
	for codelen := 1; codelen <= 32; codelen++ {
		minv := binary.BigEndian.Uint32(huff[off2+(codelen-1)*8:])
		maxv := binary.BigEndian.Uint32(huff[off2+(codelen-1)*8+4:])
		h.mincode[codelen] = uint64(minv) << (32 - codelen)
		h.maxcode[codelen] = ((uint64(maxv) + 1) << (32 - codelen)) - 1
	}
	return nil
}

func (h *huffReader) loadCDIC(cdic []byte) error {
	if len(cdic) < 16 || string(cdic[:4]) != "CDIC" {
		return fmt.Errorf("CDIC 记录无效")
	}
	phrases := int(binary.BigEndian.Uint32(cdic[8:12]))
	bits := int(binary.BigEndian.Uint32(cdic[12:16]))
	if bits < 0 || bits > 20 {
		return fmt.Errorf("CDIC 位数无效")
	}
	n := (1 << bits)
	if remain := phrases - len(h.dictionary); remain < n {
		n = remain
	}
	if n <= 0 {
		return nil
	}
	if 16+2*n > len(cdic) {
		return fmt.Errorf("CDIC 偏移表越界")
	}
	for i := 0; i < n; i++ {
		off := int(binary.BigEndian.Uint16(cdic[16+i*2:]))
		at := 16 + off
		if at+2 > len(cdic) {
			return fmt.Errorf("CDIC 条目偏移越界")
		}
		blen := binary.BigEndian.Uint16(cdic[at:])
		size := int(blen & 0x7FFF)
		start := at + 2
		end := start + size
		if end > len(cdic) {
			return fmt.Errorf("CDIC 短语越界")
		}
		h.dictionary = append(h.dictionary, huffPhrase{
			data:    cdic[start:end],
			literal: blen&0x8000 != 0,
		})
	}
	return nil
}

func (h *huffReader) unpack(data []byte) ([]byte, error) {
	return h.unpackDepth(data, 0, 64<<20)
}

func (h *huffReader) unpackDepth(data []byte, depth, limit int) ([]byte, error) {
	if depth > 32 {
		return nil, fmt.Errorf("HUFF/CDIC 递归过深")
	}
	br := &bitReader{data: data}
	totalBits := len(data) * 8
	consumed := 0
	out := make([]byte, 0, len(data)*2)
	for consumed < totalBits {
		code := br.peek32()
		e := h.dict1[int(code>>24)]
		codelen := e.codelen
		term := e.term
		maxcode := e.maxcode
		if !term {
			for codelen <= 32 && uint64(code) < h.mincode[codelen] {
				codelen++
			}
			if codelen > 32 {
				return nil, fmt.Errorf("HUFF 码字无法解码")
			}
			maxcode = h.maxcode[codelen]
		}
		if codelen <= 0 || codelen > 32 {
			return nil, fmt.Errorf("HUFF 码长无效")
		}
		if consumed+codelen > totalBits {
			break
		}
		br.consume(codelen)
		consumed += codelen
		r := int((maxcode - uint64(code)) >> (32 - codelen))
		if r < 0 || r >= len(h.dictionary) {
			return nil, fmt.Errorf("HUFF 字典索引越界")
		}
		phrase := &h.dictionary[r]
		if !phrase.done {
			if phrase.literal {
				phrase.decoded = phrase.data
			} else {
				decoded, err := h.unpackDepth(phrase.data, depth+1, limit)
				if err != nil {
					return nil, err
				}
				phrase.decoded = decoded
			}
			phrase.done = true
		}
		out = append(out, phrase.decoded...)
		if limit > 0 && len(out) > limit {
			return nil, fmt.Errorf("HUFF/CDIC 解压结果超过限制")
		}
	}
	return out, nil
}

type bitReader struct {
	data  []byte
	pos   int
	acc   uint64
	nbits uint
}

func (b *bitReader) peek32() uint32 {
	for b.nbits < 32 {
		var v byte
		if b.pos < len(b.data) {
			v = b.data[b.pos]
		}
		b.pos++
		b.acc = (b.acc << 8) | uint64(v)
		b.nbits += 8
	}
	return uint32(b.acc >> (b.nbits - 32))
}

func (b *bitReader) consume(n int) {
	if n >= int(b.nbits) {
		b.nbits = 0
		b.acc = 0
		return
	}
	b.nbits -= uint(n)
	if b.nbits == 0 {
		b.acc = 0
		return
	}
	b.acc &= (uint64(1) << b.nbits) - 1
}
