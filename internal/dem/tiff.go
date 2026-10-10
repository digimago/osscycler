package dem

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// TIFF tags read by DecodeGeoTIFF.
const (
	tagWidth           = 256
	tagHeight          = 257
	tagBitsPerSample   = 258
	tagCompression     = 259
	tagStripOffsets    = 273
	tagSamplesPerPixel = 277
	tagRowsPerStrip    = 278
	tagStripByteCounts = 279
	tagPlanarConfig    = 284
	tagPredictor       = 317
	tagTileWidth       = 322
	tagTileHeight      = 323
	tagTileOffsets     = 324
	tagTileByteCounts  = 325
	tagSampleFormat    = 339
	tagPixelScale      = 33550
	tagTiepoint        = 33922
	tagGDALNoData      = 42113
)

// DecodeGeoTIFF reads a single-band float32 GeoTIFF, as elevation services
// send them: uncompressed or deflate, with or without the floating-point
// predictor, in strips or tiles. The grid is placed by the file's tiepoint
// and pixel scale, read as the top-left corner of the first pixel (GDAL's
// PixelIsArea, which WCS servers write); cells equal to the GDAL no-data
// value become NaN.
func DecodeGeoTIFF(b []byte) (*Grid, error) {
	if len(b) < 8 {
		return nil, errors.New("tiff: too short")
	}
	var bo binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, errors.New("tiff: not a TIFF file")
	}
	if bo.Uint16(b[2:]) != 42 {
		return nil, errors.New("tiff: BigTIFF or not a TIFF file")
	}
	tags, err := readIFD(b, bo, bo.Uint32(b[4:]))
	if err != nil {
		return nil, err
	}

	one := func(tag uint16, def uint64) uint64 {
		if v := tags[tag].ints; len(v) > 0 {
			return v[0]
		}
		return def
	}
	w, h := int(one(tagWidth, 0)), int(one(tagHeight, 0))
	if w <= 0 || h <= 0 || w*h > 64<<20 {
		return nil, fmt.Errorf("tiff: size %d×%d not supported", w, h)
	}
	if one(tagSamplesPerPixel, 1) != 1 || one(tagBitsPerSample, 0) != 32 || one(tagSampleFormat, 1) != 3 {
		return nil, errors.New("tiff: only single-band float32 is supported")
	}
	if one(tagPlanarConfig, 1) != 1 {
		return nil, errors.New("tiff: planar configuration not supported")
	}
	compression, predictor := one(tagCompression, 1), one(tagPredictor, 1)
	if compression != 1 && compression != 8 && compression != 32946 {
		return nil, fmt.Errorf("tiff: compression %d not supported", compression)
	}
	if predictor != 1 && predictor != 3 {
		return nil, fmt.Errorf("tiff: predictor %d not supported", predictor)
	}

	// Strips are tiles as wide as the image.
	tw, th := w, int(one(tagRowsPerStrip, uint64(h)))
	offsets, counts := tags[tagStripOffsets].ints, tags[tagStripByteCounts].ints
	if _, tiled := tags[tagTileWidth]; tiled {
		tw, th = int(one(tagTileWidth, 0)), int(one(tagTileHeight, 0))
		offsets, counts = tags[tagTileOffsets].ints, tags[tagTileByteCounts].ints
	}
	if tw <= 0 || th <= 0 {
		return nil, errors.New("tiff: bad strip or tile size")
	}
	across, down := (w+tw-1)/tw, (h+th-1)/th
	if len(offsets) != across*down || len(counts) != len(offsets) {
		return nil, fmt.Errorf("tiff: %d blocks listed, want %d", len(offsets), across*down)
	}

	g := &Grid{W: w, H: h, Z: make([]float32, w*h)}
	block := make([]byte, tw*th*4)
	for k := range offsets {
		off, n := offsets[k], counts[k]
		if off+n > uint64(len(b)) {
			return nil, errors.New("tiff: block past the end of the file")
		}
		raw := b[off : off+n]
		// The last strip may be short.
		rows := th
		if tw == w && (k+1)*th > h {
			rows = h - k*th
		}
		want := block[:tw*rows*4]
		if compression == 1 {
			if len(raw) < len(want) {
				return nil, errors.New("tiff: short block")
			}
			copy(want, raw)
		} else {
			zr, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				return nil, fmt.Errorf("tiff: block %d: %w", k, err)
			}
			_, err = io.ReadFull(zr, want)
			zr.Close()
			if err != nil {
				return nil, fmt.Errorf("tiff: block %d: %w", k, err)
			}
		}
		x0, y0 := (k%across)*tw, (k/across)*th
		for r := range rows {
			row := want[r*tw*4 : (r+1)*tw*4]
			if predictor == 3 {
				undoFloatPredictor(row, tw)
			}
			y := y0 + r
			if y >= h {
				break
			}
			for c := 0; c < tw && x0+c < w; c++ {
				var bits uint32
				if predictor == 3 {
					// The predictor stores each value's bytes most significant first.
					bits = binary.BigEndian.Uint32(row[c*4:])
				} else {
					bits = bo.Uint32(row[c*4:])
				}
				g.Z[y*w+x0+c] = math.Float32frombits(bits)
			}
		}
	}

	scale, tie := tags[tagPixelScale].floats, tags[tagTiepoint].floats
	if len(scale) < 2 || len(tie) < 6 || scale[0] <= 0 || scale[1] <= 0 {
		return nil, errors.New("tiff: no georeference (pixel scale and tiepoint)")
	}
	g.DX, g.DY = scale[0], scale[1]
	g.X0 = tie[3] - tie[0]*g.DX
	g.Y0 = tie[4] + tie[1]*g.DY
	if nd := tags[tagGDALNoData].text; nd != "" {
		v, err := strconv.ParseFloat(strings.TrimSpace(nd), 64)
		if err == nil {
			for i, z := range g.Z {
				if float64(z) == float64(float32(v)) {
					g.Z[i] = float32(math.NaN())
				}
			}
		}
	}
	return g, nil
}

// undoFloatPredictor reverses TIFF predictor 3 on one row of n float32
// values: the bytes were split into planes (most significant first) and
// differenced as one byte sequence.
func undoFloatPredictor(row []byte, n int) {
	for i := 1; i < len(row); i++ {
		row[i] += row[i-1]
	}
	planes := append([]byte(nil), row...)
	for c := range n {
		for p := range 4 {
			row[c*4+p] = planes[p*n+c]
		}
	}
}

type tagValue struct {
	ints   []uint64
	floats []float64
	text   string
}

func readIFD(b []byte, bo binary.ByteOrder, off uint32) (map[uint16]tagValue, error) {
	if uint64(off)+2 > uint64(len(b)) {
		return nil, errors.New("tiff: bad directory offset")
	}
	n := int(bo.Uint16(b[off:]))
	if uint64(off)+2+uint64(n)*12 > uint64(len(b)) {
		return nil, errors.New("tiff: directory past the end of the file")
	}
	sizes := map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 12: 8, 16: 8}
	tags := map[uint16]tagValue{}
	for i := range n {
		e := b[int(off)+2+i*12:]
		tag, typ, count := bo.Uint16(e), bo.Uint16(e[2:]), uint64(bo.Uint32(e[4:]))
		size, ok := sizes[typ]
		if !ok {
			continue // types we never need
		}
		total := uint64(size) * count
		data := e[8:12]
		if total > 4 {
			at := uint64(bo.Uint32(e[8:]))
			if at+total > uint64(len(b)) {
				return nil, fmt.Errorf("tiff: tag %d past the end of the file", tag)
			}
			data = b[at : at+total]
		}
		var v tagValue
		for j := range count {
			switch typ {
			case 1:
				v.ints = append(v.ints, uint64(data[j]))
			case 3:
				v.ints = append(v.ints, uint64(bo.Uint16(data[j*2:])))
			case 4:
				v.ints = append(v.ints, uint64(bo.Uint32(data[j*4:])))
			case 16:
				v.ints = append(v.ints, bo.Uint64(data[j*8:]))
			case 12:
				v.floats = append(v.floats, math.Float64frombits(bo.Uint64(data[j*8:])))
			}
		}
		if typ == 2 {
			v.text = strings.TrimRight(string(data[:count]), "\x00")
		}
		tags[tag] = v
	}
	return tags, nil
}
