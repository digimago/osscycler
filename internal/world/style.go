package world

import "github.com/digimago/osscycler/internal/scenery"

// Regional styles (owner, 2026-10-15: a world in the Vosges looked like a
// Dutch village in the mountains): what grows where, what gardens, houses
// and roads look like, by the country a course is in. The shapes, the
// placing and everything built from the map are shared; a style only
// chooses among them. The Dutch style is the one the builder grew up with
// (its worlds are byte-identical to before styles); other places take it
// until they get their own.
type style struct {
	name string
	// species: the trees of each land use (speciesMix); town and townWater
	// a town's street and park trees, by water those that like it wet.
	species         map[scenery.Land]speciesMix
	town, townWater map[string]float64
	pioneer         string // the young tree that comes up where a stand lets light in
	bushes          map[scenery.Land]map[string]float64
	avenue          map[string]float64      // along roads through open land
	countryAvenue   map[string]float64      // along minor country roads
	canalAvenue     map[string]float64      // along those with water beside them
	townAvenuesOnly bool                    // no avenues in open country, only in and by towns
	willows         bool                    // pollard willows along ditches between fields
	medianHedges    bool                    // a low hedge between a road and its service road
	gardens         [hedgeKindCount]float64 // front gardens' hedges and walls: the share of plots with each
	meadowGates     bool                    // a steel five-bar gate in a long meadow fence
	pasture         pastureFence            // meadow fences' posts and wire
	walls, roofs    []uint32                // houses
	flatWalls       []uint32                // flats, offices, shops
	barnWalls       []uint32                // barns and sheds
	barnRoofs       []uint32                //
	// cover: the terrain's colour for these lands instead of the shared
	// palette's.
	cover          map[scenery.Land]uint32
	reliefM        float64 // the meso relief's strength on slopes, m (0: none; gland.relief)
	dryAboveM      float64 // grass dries towards straw above this height (0: never; Manifest.Ground.DryAboveM)
	junctionLevelM float64 // a junction reaches along a road alongside only while within this in height (0: any)
	walkGuessRank  int     // sidewalks guessed from built-up land only on roads of this class rank or higher (0: all)
	cornerWalk     bool    // sidewalks end square, and where only one road at a junction has one it runs on to the corner (else they ramp down, and go halfway round)
	roadside       bool    // barriers, retaining walls, rock netting, bend posts, kilometre markers (roadside.go)
	quays          bool    // stone quays along rivers in town (quays.go)
	gableToView    bool    // houses on a slope turn a gable to the valley (buildings.go)
	gableWood      uint32  // gables clad in timber of this colour (0: the wall's; the renderer draws the boards)
	// rbIslandMinR: roundabouts with a ring this big (centre line radius)
	// have an island, traffic going round it (0: islandMinR); rbApronM
	// its apron of setts round the island.
	rbIslandMinR, rbApronM float64
	markings               bool    // white centre and edge lines
	snowPoleAboveM         float64 // snow poles along roads above this height (0: none)
	cycleLane              uint32  // a cycle lane's surface
	roundaboutCentre       uint32  // a flat roundabout's paved centre
	// bands: by height above the datum, the trees that grow there
	// (replacing species for the lands a band names) and how tall they
	// get; the last band reaches up without end. None: no bands.
	bands []altitudeBand
}

// altitudeBand is a height band's trees.
type altitudeBand struct {
	below   float64 // m above the datum: this band is below it (and above the band before)
	species map[scenery.Land]speciesMix
	size    float64 // the trees' height factor (wind and cold keep them low)
	// openSize: solitary trees on meadows and open land grow this much
	// smaller again (0: as size); above the treeline they are bushes.
	openSize float64
}

// landColour is the terrain's colour for land l: the style's own where
// it has one, else the shared palette's.
func (st *style) landColour(l scenery.Land) [3]float32 {
	if c, ok := st.cover[l]; ok {
		return srgbLinear(c)
	}
	return landColors[l]
}

// band is the altitude band at height z; nil for a style without bands.
func (st *style) band(z float64) *altitudeBand {
	for i := range st.bands {
		if z < st.bands[i].below || i == len(st.bands)-1 {
			return &st.bands[i]
		}
	}
	return nil
}

// speciesFor is the mix of land l at height z.
func (st *style) speciesFor(l scenery.Land, z float64) (speciesMix, bool) {
	if b := st.band(z); b != nil {
		if m, ok := b.species[l]; ok {
			return m, true
		}
	}
	m, ok := st.species[l]
	return m, ok
}

// Regions: the style names Options.Region takes.
const (
	RegionNL = "nl" // the default
	RegionFR = "fr"
)

// styleFor is region's style, the Dutch one for "" or a region without
// its own.
func styleFor(region string) *style {
	if region == RegionFR {
		return &styleFR
	}
	return &styleNL
}

var styleNL = style{
	name:          RegionNL,
	species:       speciesByLand,
	town:          townMix,
	townWater:     townWaterMix,
	pioneer:       "birch",
	bushes:        bushesByLand,
	avenue:        avenueMix,
	countryAvenue: countryAvenueMix,
	canalAvenue:   canalAvenueMix,
	willows:       true,
	medianHedges:  true,
	gardens:       gardenMix,
	meadowGates:   true,
	pasture:       keptFence,
	walls:         brickWalls, roofs: tileRoofs,
	flatWalls: flatWalls,
	barnWalls: barnWalls, barnRoofs: barnRoofs,
	cycleLane:        0xa04a3c, // red, as Dutch fietsstroken
	roundaboutCentre: 0x8c5a48, // red klinkers
}

// The Vosges (owner's reference shots, 2026-10-15: La Bresse's Rue Paul
// Claudel and Rue de l'Église, the Route des Américains at ~1000 m):
// rendered houses in pale grey, cream and pale yellow under red-brown or
// dark grey tiles; lime trees along the streets; front gardens behind a
// low wall, a railing or thuja; dense spruce and silver fir to the road's
// edge on the mountain; beech and fir lower down; the crest's pastures
// (the chaumes, above ~1200 m) open tawny grass with a few wind-shorn
// beech and rowan as bushes (owner's shots at the Hohneck's summit road).
// Vegetation zones after the Vosges' (collinéen, montagnard,
// subalpin): oak and beech below ~600 m, beech-fir forest with planted
// spruce to ~1100 m, stunted beech and rowan above.
var styleFR = style{
	name: RegionFR,
	species: map[scenery.Land]speciesMix{
		scenery.LandForest: {
			broad:   map[string]float64{"oak": 35, "beech": 35, "ash": 10, "maple": 10, "birch": 10},
			conifer: map[string]float64{"spruce": 45, "silver fir": 35, "pine": 20},
		},
		scenery.LandHeath: {
			broad:   map[string]float64{"birch": 60, "rowan": 40},
			conifer: map[string]float64{"pine": 60, "spruce": 40},
		},
		scenery.LandBuilt: {
			broad:   map[string]float64{"birch": 25, "cherry": 25, "maple": 20, "ash": 15, "lime": 15},
			conifer: map[string]float64{"spruce": 60, "fir": 25, "pine": 15},
		},
		scenery.LandMeadow: {broad: map[string]float64{"ash": 35, "maple": 30, "oak": 20, "beech": 15}},
		scenery.LandNone:   {broad: map[string]float64{"ash": 35, "maple": 30, "oak": 20, "beech": 15}},
	},
	// Limes (tilleuls) as on the Rue Paul Claudel; few planes this far
	// north and east.
	town:      map[string]float64{"lime": 40, "maple": 20, "ash": 10, "birch": 8, "cherry": 8, "plane": 8, "chestnut": 6},
	townWater: map[string]float64{"alder": 40, "ash": 25, "poplar": 20, "birch": 15},
	pioneer:   "rowan",
	bushes: map[scenery.Land]map[string]float64{
		scenery.LandForest: {"hazel": 35, "holly": 15, "hawthorn": 15, "bramble": 15, "elder": 10, "currant": 10},
		scenery.LandHeath:  {"broom": 50, "juniper": 30, "hawthorn": 20},
		scenery.LandBuilt:  {"laurel": 30, "box": 20, "hazel": 10, "currant": 10, "hawthorn": 10, "holly": 10, "rhododendron": 10},
		scenery.LandMeadow: {"hawthorn": 45, "hazel": 35, "elder": 10, "bramble": 10},
		scenery.LandNone:   {"hawthorn": 45, "hazel": 35, "elder": 10, "bramble": 10},
	},
	avenue:          map[string]float64{"lime": 45, "plane": 30, "maple": 25},
	countryAvenue:   map[string]float64{"lime": 45, "plane": 30, "maple": 25},
	canalAvenue:     map[string]float64{"poplar": 50, "ash": 25, "alder": 25},
	townAvenuesOnly: true, // mountain roads run through the woods and the pastures as they are
	// Thuja (the conifer hedge) above all, some clipped hornbeam
	// (charmille); low walls come with the street furniture.
	gardens: [hedgeKindCount]float64{hHornbeam: 0.06, hHornbeamTall: 0.03, hConifer: 0.22, hWall: 0.3},
	// Rendered walls (crépi), as on the reference shots: pale grey,
	// cream, pale yellow, peach; the odd one in sandstone or with timber
	// cladding.
	walls:     []uint32{0xd9d6cf, 0xe6dcc3, 0xe3d29a, 0xebe7de, 0xcfc0a0, 0xe0c4bc, 0xd6b98a, 0xe8b48e, 0xe8d49a, 0xa88a6e},
	roofs:     []uint32{0x8a4a3a, 0x7a3c30, 0x8a4a3a, 0x6a4a3c, 0x4a4a4c, 0x3a3c40},
	flatWalls: []uint32{0xd9d6cf, 0xe6dcc3, 0xcfc8b8, 0xb8b0a0},
	// Vosges farms: render below, dark timber cladding above.
	barnWalls: []uint32{0x7a6650, 0xbfb49c, 0x5e4a3a, 0x6e5a46},
	barnRoofs: []uint32{0x5a5a5c, 0x7a4a3a, 0x4a4a4c},
	// The summer pastures' (owner's shots on the Hohneck's summit road):
	// weathered posts 5-6 m apart, leaning this way and that, one wire.
	pasture: pastureFence{postM: 5.5, postH: 1.0, postR: 0.06, leanDeg: 9, strands: []float64{0.8}, colour: 0x7a746a},
	// The chaumes' grass, tawny in summer (owner's shots on the
	// Hohneck's summit road), green still in the valleys.
	// Built-up land on a mountain village's slopes is gardens and meadow
	// between the houses, not paving: the shared grey read as concrete
	// walls on La Bresse's hillsides.
	cover:          map[scenery.Land]uint32{scenery.LandBuilt: 0x6f8a52},
	reliefM:        1.6,
	dryAboveM:      1150,
	roadside:       true,
	junctionLevelM: abreastStepM,
	// A French village's residential lanes and service roads have no
	// sidewalks (owner's photo of a lane off the Rue du Hohneck, 2026-10-16:
	// grass at one edge, a wall at the other); its through roads do.
	walkGuessRank: 5, // tertiary
	// And a sidewalk ends at a lane's mouth, square with it, rather than
	// turning halfway into it (the same photo: it had wrapped round into
	// the lane's mouth at the road's level, floating beside its ramp).
	cornerWalk:  true,
	quays:       true,
	gableToView: true,
	gableWood:   0x4e3626, // dark stained boards, as on the Rue du Hohneck
	// La Bresse's roundabouts (owner, 2026-10-16, marks at 0.51-0.54 km):
	// a raised, planted island inside a narrow apron of reddish setts,
	// not a flat disc ridden straight across.
	rbIslandMinR:     8,
	rbApronM:         1.5,
	markings:         true,
	snowPoleAboveM:   800,      // the jalons along the D430 and the passes
	cycleLane:        0x4c4d50, // bandes cyclables: asphalt, marked by a line
	roundaboutCentre: 0x9c6052, // reddish setts, as the aprons round La Bresse's roundabouts (owner's shots)
	bands: []altitudeBand{
		{below: 600, size: 1},
		{below: 1050, size: 1, species: map[scenery.Land]speciesMix{
			scenery.LandForest: {
				broad:   map[string]float64{"beech": 70, "maple": 20, "ash": 5, "rowan": 5},
				conifer: map[string]float64{"spruce": 60, "silver fir": 40},
			},
			scenery.LandMeadow: {broad: map[string]float64{"maple": 40, "ash": 30, "beech": 20, "rowan": 10}},
			scenery.LandNone:   {broad: map[string]float64{"maple": 40, "ash": 30, "beech": 20, "rowan": 10}},
		}},
		{below: 1150, size: 0.8, species: map[scenery.Land]speciesMix{
			scenery.LandForest: {
				broad:   map[string]float64{"beech": 45, "mountain beech": 30, "rowan": 15, "maple": 10},
				conifer: map[string]float64{"spruce": 75, "silver fir": 25},
			},
			scenery.LandMeadow: {broad: map[string]float64{"mountain beech": 60, "rowan": 25, "maple": 15}},
			scenery.LandNone:   {broad: map[string]float64{"mountain beech": 60, "rowan": 25, "maple": 15}},
		}},
		// The crest: wind-shorn beech (hêtraie d'altitude) at the woods'
		// edge; on the open pasture a few beech and rowan, 1.5-3 m
		// bushes.
		{size: 0.85, openSize: 0.4, species: map[scenery.Land]speciesMix{
			scenery.LandForest: {
				broad:   map[string]float64{"mountain beech": 85, "rowan": 15},
				conifer: map[string]float64{"spruce": 100},
			},
			scenery.LandMeadow: {broad: map[string]float64{"mountain beech": 70, "rowan": 30}},
			scenery.LandNone:   {broad: map[string]float64{"mountain beech": 70, "rowan": 30}},
		}},
	},
}

// regionScenery adapts the map data to the region's style: guessed
// sidewalks dropped from roads below its walkGuessRank. The ways keep
// their order (RouteOn indexes them); the map data itself is shared, so
// it is copied, not changed.
func (o *Options) regionScenery() {
	st := styleFor(o.Region)
	if st.walkGuessRank == 0 || o.Scenery == nil {
		return
	}
	var ways []scenery.Way
	for i, w := range o.Scenery.Ways {
		if !w.WalkGuessed || classRank[w.Class] >= st.walkGuessRank {
			continue
		}
		if ways == nil {
			ways = append([]scenery.Way(nil), o.Scenery.Ways...)
		}
		ways[i].Sidewalk, ways[i].WalkGuessed = [2]bool{}, false
	}
	if ways != nil {
		sc := *o.Scenery
		sc.Ways = ways
		o.Scenery = &sc
	}
}
