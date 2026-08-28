/*
FILE: futures/types/doc.go

DESCRIPTION:
Package types (futures/types) is the data layer of the Futures profile
(USD-M perpetual futures, /fapi/v3). Protocol-common entities are ALIASED
from the neutral root types package (see aliases.go); this package adds only
futures-specific enums and structs (position side, working type, margin
type, position/balance/symbol info, request structs).

LAYERING RULES:
  - never import spot/types (sections are equal peers);
  - anything that a second section starts using moves DOWN to the neutral
    types/ package, never sideways.
*/

package types
