package gltf

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"slices"
	"testing"
)

func TestWriteGLB(t *testing.T) {
	d := New("test")
	mat := d.AddMaterial(Material{Name: "grass", Color: [4]float32{0.2, 0.5, 0.1, 1}, Roughness: 0.9})
	m, err := d.AddMesh("tri", Primitive{
		Positions: []float32{0, 0, 0, 1, 0, 0, 0, 2, -1},
		Normals:   []float32{0, 1, 0, 0, 1, 0, 0, 1, 0},
		UVs:       []float32{0, 0, 1, 0, 0, 1},
		Colors:    []float32{1, 0, 0, 0, 0.5, 0, 0, 0, 2},
		Indices:   []uint32{0, 1, 2},
		Material:  mat,
	})
	if err != nil {
		t.Fatal(err)
	}
	d.AddNode("tri", m, map[string]int{"chunk": 7})
	var buf bytes.Buffer
	if err := d.WriteGLB(&buf); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()

	if string(b[:4]) != "glTF" || binary.LittleEndian.Uint32(b[4:]) != 2 || int(binary.LittleEndian.Uint32(b[8:])) != len(b) {
		t.Fatalf("bad header % x", b[:12])
	}
	jsonLen := int(binary.LittleEndian.Uint32(b[12:]))
	if string(b[16:20]) != "JSON" || jsonLen%4 != 0 {
		t.Fatalf("bad JSON chunk header")
	}
	var g document
	if err := json.Unmarshal(b[20:20+jsonLen], &g); err != nil {
		t.Fatal(err)
	}
	bin := b[20+jsonLen:]
	binLen := int(binary.LittleEndian.Uint32(bin))
	if string(bin[4:8]) != "BIN\x00" || binLen%4 != 0 || binLen != len(bin)-8 {
		t.Fatalf("bad BIN chunk")
	}
	if len(g.Buffers) != 1 || g.Buffers[0].ByteLength > binLen {
		t.Fatalf("buffers %+v for a %d byte chunk", g.Buffers, binLen)
	}
	for i, v := range g.BufferViews {
		if v.ByteOffset%4 != 0 || v.ByteOffset+v.ByteLength > g.Buffers[0].ByteLength {
			t.Errorf("view %d %+v misplaced", i, v)
		}
	}
	pos := g.Accessors[g.Meshes[0].Primitives[0].Attributes["POSITION"]]
	if pos.Count != 3 || pos.Min[1] != 0 || pos.Max[1] != 2 || pos.Min[2] != -1 {
		t.Errorf("position accessor %+v", pos)
	}
	// The second vertex's x reads back from the buffer.
	at := 8 + g.BufferViews[pos.BufferView].ByteOffset + 12
	if x := math.Float32frombits(binary.LittleEndian.Uint32(bin[at:])); x != 1 {
		t.Errorf("vertex 1 x = %v", x)
	}
	// Colours as normalized RGBA bytes, clamped.
	col := g.Accessors[g.Meshes[0].Primitives[0].Attributes["COLOR_0"]]
	if col.ComponentType != compUByte || !col.Normalized || col.Type != "VEC4" || col.Count != 3 {
		t.Errorf("colour accessor %+v", col)
	}
	at = 8 + g.BufferViews[col.BufferView].ByteOffset
	if got := bin[at : at+12]; !bytes.Equal(got, []byte{255, 0, 0, 255, 0, 128, 0, 255, 0, 0, 255, 255}) {
		t.Errorf("colours % x", got)
	}
	// Normals as normalized signed bytes, 4 a vertex, under the extension.
	nor := g.Accessors[g.Meshes[0].Primitives[0].Attributes["NORMAL"]]
	if nor.ComponentType != compByte || !nor.Normalized || nor.Type != "VEC3" || g.BufferViews[nor.BufferView].ByteStride != 4 {
		t.Errorf("normal accessor %+v", nor)
	}
	at = 8 + g.BufferViews[nor.BufferView].ByteOffset
	if got := bin[at : at+4]; !bytes.Equal(got, []byte{0, 127, 0, 0}) {
		t.Errorf("normal % x", got)
	}
	if !slices.Equal(g.ExtensionsRequired, []string{"KHR_mesh_quantization"}) {
		t.Errorf("extensions required %v", g.ExtensionsRequired)
	}
	idx := g.Accessors[g.Meshes[0].Primitives[0].Indices]
	if idx.ComponentType != compUShort || idx.Count != 3 {
		t.Errorf("index accessor %+v", idx)
	}
	if len(g.Scenes[0].Nodes) != 1 || g.Nodes[0].Extras == nil {
		t.Errorf("scene %+v nodes %+v", g.Scenes, g.Nodes)
	}
}

func TestAddMeshChecks(t *testing.T) {
	d := New("test")
	mat := d.AddMaterial(Material{})
	for name, p := range map[string]Primitive{
		"index past the end": {Positions: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, Indices: []uint32{0, 1, 3}, Material: mat},
		"no material":        {Positions: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, Indices: []uint32{0, 1, 2}, Material: 5},
		"short normals":      {Positions: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, Normals: []float32{0, 1, 0}, Indices: []uint32{0, 1, 2}, Material: mat},
		"not triangles":      {Positions: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, Indices: []uint32{0, 1}, Material: mat},
	} {
		if _, err := d.AddMesh(name, p); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestWeld(t *testing.T) {
	// Two triangles as pieces, each with its own copies of the shared
	// edge; one copy's normal differs below a byte's precision, a third
	// vertex's colour by a whole step.
	p := weld(Primitive{
		Positions: []float32{0, 0, 0, 1, 0, 0, 0, 0, -1, 1, 0, 0, 1, 0, -1, 0, 0, -1, 0, 0, 0},
		Normals:   []float32{0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0.001, 1, 0, 0, 1, 0},
		Colors:    []float32{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0.5, 1, 1},
		Indices:   []uint32{0, 1, 2, 3, 4, 5, 6, 3, 5},
	})
	if got := len(p.Positions) / 3; got != 5 {
		t.Errorf("%d vertices, want 5 (the shared edge's copies welded, the other colour kept)", got)
	}
	if !slices.Equal(p.Indices[:6], []uint32{0, 1, 2, 1, 3, 2}) || p.Indices[6] != 4 {
		t.Errorf("indices %v", p.Indices)
	}
}
