/*
FILE: types/doc.go

DESCRIPTION:
Package types is the NEUTRAL protocol layer of the SDK: enums and data
structures shared by the futures and (future) spot profiles. Section packages
(futures/types, spot/types) are equal peers that ALIAS these entities and
extend them with profile-specific values — they never import each other.

LAYERING RULES:
  - types/ holds only entities whose wire format is identical across sections
    (Aster follows the Binance convention, so sides, order types, TIF, order
    statuses, book levels, candles and trade events coincide);
  - anything section-specific (PositionSide, WorkingType, MarginType, position
    and balance structs) lives in <section>/types;
  - moving an entity is allowed only DOWN (section → neutral) when a second
    section starts using it, never sideways.
*/

package types
