package main

import "fmt"

type Geometry struct {
	X   int   `json:"x" mapstructure:"x"`
	Y   int   `json:"y" mapstructure:"y"`
	W   int   `json:"w" mapstructure:"w"`
	H   int   `json:"h" mapstructure:"h"`
}

func (geo *Geometry) String() string {
        return fmt.Sprintf("%dx%d+%d+%d",geo.W,geo.H,geo.X,geo.Y)
}
