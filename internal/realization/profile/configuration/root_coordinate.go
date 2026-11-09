package configuration

import (
	"fmt"
	"path"
	"strings"
)

type RootCoordinateKind uint8

const (
	CoordinateDirectoryChild RootCoordinateKind = iota + 1
	CoordinatePointSelf
)

// RootCoordinate distinguishes a child of a selected directory from the selected
// file point itself. Neither form names or authorizes the point's witness parent.
type RootCoordinate struct {
	kind  RootCoordinateKind
	child string
}

func NewDirectoryChild(relative string) (RootCoordinate, error) {
	coordinate := RootCoordinate{kind: CoordinateDirectoryChild, child: relative}
	if err := coordinate.Validate(); err != nil {
		return RootCoordinate{}, err
	}
	return coordinate, nil
}

func PointSelf() RootCoordinate {
	return RootCoordinate{kind: CoordinatePointSelf}
}

func (coordinate RootCoordinate) Kind() RootCoordinateKind { return coordinate.kind }

func (coordinate RootCoordinate) DirectoryChild() (string, bool) {
	return coordinate.child, coordinate.kind == CoordinateDirectoryChild
}

func (coordinate RootCoordinate) Validate() error {
	switch coordinate.kind {
	case CoordinateDirectoryChild:
		child := coordinate.child
		drive := len(child) >= 2 && child[1] == ':' && (child[0] >= 'a' && child[0] <= 'z' || child[0] >= 'A' && child[0] <= 'Z')
		if child == "" || child == "." || child == ".." || strings.ContainsAny(child, "\\\x00") ||
			path.IsAbs(child) || drive || strings.HasPrefix(child, "../") || path.Clean(child) != child {
			return fmt.Errorf("directory-child coordinate must be canonical and stay below its selected root")
		}
		return nil
	case CoordinatePointSelf:
		if coordinate.child != "" {
			return fmt.Errorf("file-point self coordinate cannot contain a child path")
		}
		return nil
	default:
		return fmt.Errorf("native root coordinate kind is required")
	}
}
