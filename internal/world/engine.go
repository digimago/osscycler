package world

// One world builder (owner, 2026-10-21: the classic builder retired, the
// graph builder ridden end to end on the Posbank Loop and the Hohneck
// loop): Build (graph.go) lays the road network first, as a graph; heights
// solved on it, the course as a way through it, the surfaces from the
// junctions out, and the land last, laid round the roads' outline. The
// terrain-first builder it replaced, its history and its A/B numbers are in
// docs/HISTORY.md ("Two world builders").
//
// Engine is the builder's name in the manifest (Manifest.Builder), as
// worlds built while there were two say which made them.
const Engine = "graph"
