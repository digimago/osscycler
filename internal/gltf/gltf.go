// Package gltf writes glTF 2.0 binary files (.glb): triangle meshes with
// positions, normals, optional texture coordinates and colours, plain PBR
// materials and a flat node list. It is the small subset osscycler's world
// builder needs, with no dependency.
//
// glTF's frame: metres, right-handed, +Y up; front faces wind counter-
// clockwise.
package gltf

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"slices"
)

// Doc is a glTF document under construction. Everything goes into one
// binary buffer and one scene.
type Doc struct {
	g   document
	bin []byte
}

// Primitive is a triangle mesh. Positions, normals and colours are flat
// xyz / rgb triples, UVs uv pairs; Normals, UVs and Colors are optional
// (nil). Colours are linear, 0 to 1, and stored as bytes (a quarter of
// the size of floats). Indices list three vertices per triangle.
type Primitive struct {
	Positions []float32
	Normals   []float32
	UVs       []float32
	Colors    []float32
	Indices   []uint32
	Material  int
}

// Material is a metallic-roughness material without textures.
type Material struct {
	Name        string
	Color       [4]float32 // linear RGBA
	Metallic    float32
	Roughness   float32
	DoubleSided bool
}

// New starts a document; generator names the program that writes it.
func New(generator string) *Doc {
	d := &Doc{}
	d.g.Asset = asset{Version: "2.0", Generator: generator}
	d.g.Scenes = []scene{{Nodes: []int{}}}
	d.g.Scene = 0
	return d
}

// AddMaterial adds a material and returns its index.
func (d *Doc) AddMaterial(m Material) int {
	d.g.Materials = append(d.g.Materials, material{
		Name: m.Name,
		PBR: pbr{
			BaseColorFactor: m.Color,
			MetallicFactor:  m.Metallic,
			RoughnessFactor: m.Roughness,
		},
		DoubleSided: m.DoubleSided,
	})
	return len(d.g.Materials) - 1
}

// MaterialName is the name of material i.
func (d *Doc) MaterialName(i int) string {
	if i < 0 || i >= len(d.g.Materials) {
		return ""
	}
	return d.g.Materials[i].Name
}

// AddMesh adds a mesh of one or more primitives and returns its index.
func (d *Doc) AddMesh(name string, prims ...Primitive) (int, error) {
	m := mesh{Name: name}
	for _, p := range prims {
		n := len(p.Positions) / 3
		switch {
		case n == 0 || len(p.Positions)%3 != 0:
			return 0, errors.New("gltf: positions must be xyz triples")
		case p.Normals != nil && len(p.Normals) != 3*n:
			return 0, errors.New("gltf: one normal per vertex")
		case p.UVs != nil && len(p.UVs) != 2*n:
			return 0, errors.New("gltf: one UV per vertex")
		case p.Colors != nil && len(p.Colors) != 3*n:
			return 0, errors.New("gltf: one colour per vertex")
		case len(p.Indices) == 0 || len(p.Indices)%3 != 0:
			return 0, errors.New("gltf: indices must be triangles")
		case p.Material < 0 || p.Material >= len(d.g.Materials):
			return 0, errors.New("gltf: no such material")
		}
		for _, i := range p.Indices {
			if int(i) >= n {
				return 0, errors.New("gltf: index past the vertices")
			}
		}
		p = weld(p)
		n = len(p.Positions) / 3
		attrs := map[string]int{"POSITION": d.floats(p.Positions, "VEC3", 3, true)}
		if p.Normals != nil {
			attrs["NORMAL"] = d.normals(p.Normals)
		}
		if p.UVs != nil {
			attrs["TEXCOORD_0"] = d.floats(p.UVs, "VEC2", 2, false)
		}
		if p.Colors != nil {
			attrs["COLOR_0"] = d.colors(p.Colors)
		}
		mat := p.Material
		m.Primitives = append(m.Primitives, primitive{
			Attributes: attrs,
			Indices:    d.indices(p.Indices, n),
			Material:   &mat,
			Mode:       4, // triangles
		})
	}
	d.g.Meshes = append(d.g.Meshes, m)
	return len(d.g.Meshes) - 1, nil
}

// weld merges vertices that are the same as stored: position and UV to
// the float32 bit, normal and colour to the byte (meshes clipped into
// pieces repeat their vertices; welded, the file is smaller and nothing
// changes in it).
func weld(p Primitive) Primitive {
	type key struct {
		pos [3]uint32
		uv  [2]uint32
		nor [3]int8
		col [3]byte
	}
	n := len(p.Positions) / 3
	out := Primitive{Material: p.Material}
	index := make(map[key]uint32, n)
	remap := make([]uint32, n)
	for v := range n {
		var k key
		for c := range 3 {
			k.pos[c] = math.Float32bits(p.Positions[3*v+c])
			if p.Normals != nil {
				k.nor[c] = normalByte(p.Normals[3*v+c])
			}
			if p.Colors != nil {
				k.col[c] = colorByte(p.Colors[3*v+c])
			}
		}
		if p.UVs != nil {
			k.uv = [2]uint32{math.Float32bits(p.UVs[2*v]), math.Float32bits(p.UVs[2*v+1])}
		}
		w, ok := index[k]
		if !ok {
			w = uint32(len(out.Positions) / 3)
			index[k] = w
			out.Positions = append(out.Positions, p.Positions[3*v:3*v+3]...)
			if p.Normals != nil {
				out.Normals = append(out.Normals, p.Normals[3*v:3*v+3]...)
			}
			if p.Colors != nil {
				out.Colors = append(out.Colors, p.Colors[3*v:3*v+3]...)
			}
			if p.UVs != nil {
				out.UVs = append(out.UVs, p.UVs[2*v:2*v+2]...)
			}
		}
		remap[v] = w
	}
	out.Indices = make([]uint32, len(p.Indices))
	for i, v := range p.Indices {
		out.Indices[i] = remap[v]
	}
	return out
}

func normalByte(f float32) int8 { return int8(math.Round(float64(max(-1, min(1, f))) * 127)) }

func colorByte(f float32) byte { return byte(math.Round(float64(max(0, min(1, f))) * 255)) }

// AddNode places a mesh in the scene, untransformed. Extras are free-form
// data for the reader (glTF's "extras"); nil leaves them out.
func (d *Doc) AddNode(name string, mesh int, extras any) int {
	d.g.Nodes = append(d.g.Nodes, node{Name: name, Mesh: &mesh, Extras: extras})
	i := len(d.g.Nodes) - 1
	d.g.Scenes[0].Nodes = append(d.g.Scenes[0].Nodes, i)
	return i
}

// WriteGLB writes the document as a binary glTF file.
func (d *Doc) WriteGLB(w io.Writer) error {
	g := d.g
	if len(d.bin) > 0 {
		g.Buffers = []buffer{{ByteLength: len(d.bin)}}
	}
	js, err := json.Marshal(g)
	if err != nil {
		return err
	}
	js = pad(js, ' ')
	bin := pad(d.bin, 0)
	total := 12 + 8 + len(js)
	if len(bin) > 0 {
		total += 8 + len(bin)
	}
	if uint64(total) > math.MaxUint32 {
		return errors.New("gltf: file over 4 GiB")
	}
	var head [12]byte
	copy(head[:], "glTF")
	binary.LittleEndian.PutUint32(head[4:], 2)
	binary.LittleEndian.PutUint32(head[8:], uint32(total))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	if err := chunk(w, 0x4E4F534A, js); err != nil { // "JSON"
		return err
	}
	if len(bin) > 0 {
		return chunk(w, 0x004E4942, bin) // "BIN\0"
	}
	return nil
}

func chunk(w io.Writer, typ uint32, b []byte) error {
	var h [8]byte
	binary.LittleEndian.PutUint32(h[:], uint32(len(b)))
	binary.LittleEndian.PutUint32(h[4:], typ)
	if _, err := w.Write(h[:]); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

func pad(b []byte, with byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, with)
	}
	return b
}

const (
	targetArray   = 34962
	targetElement = 34963
	compFloat     = 5126
	compByte      = 5120
	compUByte     = 5121
	compUShort    = 5123
	compUInt      = 5125
)

// floats appends a vertex attribute and returns its accessor; positions
// need their bounds.
func (d *Doc) floats(v []float32, typ string, k int, bounds bool) int {
	view := d.view(len(v)*4, targetArray)
	for _, f := range v {
		d.bin = binary.LittleEndian.AppendUint32(d.bin, math.Float32bits(f))
	}
	a := accessor{BufferView: view, ComponentType: compFloat, Count: len(v) / k, Type: typ}
	if bounds {
		lo, hi := make([]float32, k), make([]float32, k)
		for c := range k {
			lo[c], hi[c] = float32(math.Inf(1)), float32(math.Inf(-1))
		}
		for i, f := range v {
			c := i % k
			lo[c], hi[c] = min(lo[c], f), max(hi[c], f)
		}
		a.Min, a.Max = lo, hi
	}
	d.g.Accessors = append(d.g.Accessors, a)
	return len(d.g.Accessors) - 1
}

// normals appends unit normals as normalized signed bytes, padded to 4
// bytes a vertex (KHR_mesh_quantization): a third of floats, and finer
// than shading needs.
func (d *Doc) normals(v []float32) int {
	n := len(v) / 3
	view := d.view(4*n, targetArray)
	d.g.BufferViews[view].ByteStride = 4
	q := func(f float32) byte { return byte(normalByte(f)) }
	for i := range n {
		d.bin = append(d.bin, q(v[3*i]), q(v[3*i+1]), q(v[3*i+2]), 0)
	}
	d.useExtension("KHR_mesh_quantization", true)
	d.g.Accessors = append(d.g.Accessors, accessor{BufferView: view, ComponentType: compByte, Normalized: true, Count: n, Type: "VEC3"})
	return len(d.g.Accessors) - 1
}

// useExtension lists a glTF extension as used (and required).
func (d *Doc) useExtension(name string, required bool) {
	if !slices.Contains(d.g.ExtensionsUsed, name) {
		d.g.ExtensionsUsed = append(d.g.ExtensionsUsed, name)
	}
	if required && !slices.Contains(d.g.ExtensionsRequired, name) {
		d.g.ExtensionsRequired = append(d.g.ExtensionsRequired, name)
	}
}

// colors appends linear RGB colours as normalized bytes, RGBA (4 bytes a
// vertex keeps every element aligned, as glTF requires).
func (d *Doc) colors(rgb []float32) int {
	n := len(rgb) / 3
	view := d.view(4*n, targetArray)
	for i := range n {
		d.bin = append(d.bin, colorByte(rgb[3*i]), colorByte(rgb[3*i+1]), colorByte(rgb[3*i+2]), 255)
	}
	d.g.Accessors = append(d.g.Accessors, accessor{BufferView: view, ComponentType: compUByte, Normalized: true, Count: n, Type: "VEC4"})
	return len(d.g.Accessors) - 1
}

// indices appends triangle indices, as 16 bits when the vertices allow.
func (d *Doc) indices(idx []uint32, vertices int) int {
	short := vertices <= math.MaxUint16
	size := 4
	comp := compUInt
	if short {
		size, comp = 2, compUShort
	}
	view := d.view(len(idx)*size, targetElement)
	for _, i := range idx {
		if short {
			d.bin = binary.LittleEndian.AppendUint16(d.bin, uint16(i))
		} else {
			d.bin = binary.LittleEndian.AppendUint32(d.bin, i)
		}
	}
	d.g.Accessors = append(d.g.Accessors, accessor{BufferView: view, ComponentType: comp, Count: len(idx), Type: "SCALAR"})
	return len(d.g.Accessors) - 1
}

// view starts a buffer view of n bytes at a 4-byte boundary.
func (d *Doc) view(n, target int) int {
	for len(d.bin)%4 != 0 {
		d.bin = append(d.bin, 0)
	}
	d.g.BufferViews = append(d.g.BufferViews, bufferView{ByteOffset: len(d.bin), ByteLength: n, Target: target})
	return len(d.g.BufferViews) - 1
}

// The JSON side of glTF, as far as used here.
type document struct {
	Asset       asset        `json:"asset"`
	Scene       int          `json:"scene"`
	Scenes      []scene      `json:"scenes"`
	Nodes       []node       `json:"nodes,omitempty"`
	Meshes      []mesh       `json:"meshes,omitempty"`
	Materials   []material   `json:"materials,omitempty"`
	Accessors   []accessor   `json:"accessors,omitempty"`
	BufferViews []bufferView `json:"bufferViews,omitempty"`
	Buffers     []buffer     `json:"buffers,omitempty"`

	ExtensionsUsed     []string `json:"extensionsUsed,omitempty"`
	ExtensionsRequired []string `json:"extensionsRequired,omitempty"`
}

type asset struct {
	Version   string `json:"version"`
	Generator string `json:"generator,omitempty"`
}

type scene struct {
	Nodes []int `json:"nodes"`
}

type node struct {
	Name   string `json:"name,omitempty"`
	Mesh   *int   `json:"mesh,omitempty"`
	Extras any    `json:"extras,omitempty"`
}

type mesh struct {
	Name       string      `json:"name,omitempty"`
	Primitives []primitive `json:"primitives"`
}

type primitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    int            `json:"indices"`
	Material   *int           `json:"material,omitempty"`
	Mode       int            `json:"mode"`
}

type material struct {
	Name        string `json:"name,omitempty"`
	PBR         pbr    `json:"pbrMetallicRoughness"`
	DoubleSided bool   `json:"doubleSided,omitempty"`
}

type pbr struct {
	BaseColorFactor [4]float32 `json:"baseColorFactor"`
	MetallicFactor  float32    `json:"metallicFactor"`
	RoughnessFactor float32    `json:"roughnessFactor"`
}

type accessor struct {
	BufferView    int       `json:"bufferView"`
	ComponentType int       `json:"componentType"`
	Normalized    bool      `json:"normalized,omitempty"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Min           []float32 `json:"min,omitempty"`
	Max           []float32 `json:"max,omitempty"`
}

type bufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	ByteStride int `json:"byteStride,omitempty"`
	Target     int `json:"target,omitempty"`
}

type buffer struct {
	ByteLength int `json:"byteLength"`
}
