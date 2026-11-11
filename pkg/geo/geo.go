package geo

import "math"

type Location struct {
	Lat float64 `bson:"lat" json:"lat"`
	Lng float64 `bson:"lng" json:"lng"`
}

// Input distinguishes missing JSON fields from legitimate zero coordinates.
type Input struct {
	Lat *float64 `json:"lat"`
	Lng *float64 `json:"lng"`
}

func (i Input) Location() (Location, bool) {
	if i.Lat == nil || i.Lng == nil {
		return Location{}, false
	}
	location := Location{Lat: *i.Lat, Lng: *i.Lng}
	return location, location.Valid()
}

func (p Location) Valid() bool {
	return p.Lat >= -90 && p.Lat <= 90 && p.Lng >= -180 && p.Lng <= 180
}

func Kilometers(a, b Location) float64 {
	const radians = math.Pi / 180
	dlat := (b.Lat - a.Lat) * radians
	dlng := (b.Lng - a.Lng) * radians
	h := math.Pow(math.Sin(dlat/2), 2) +
		math.Cos(a.Lat*radians)*math.Cos(b.Lat*radians)*math.Pow(math.Sin(dlng/2), 2)
	return 6371 * 2 * math.Asin(math.Sqrt(min(1, h)))
}

// Point encodes the GeoJSON coordinate order used by geospatial indexes.
type Point struct {
	Type        string     `bson:"type"`
	Coordinates [2]float64 `bson:"coordinates"`
}

func (p Location) Point() Point {
	return Point{Type: "Point", Coordinates: [2]float64{p.Lng, p.Lat}}
}
