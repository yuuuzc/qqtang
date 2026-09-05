package game

import "encoding/binary"

// KinFlagID is the eight-byte KINFLAGID structure used by the original
// client. The two fields have distinct meanings and must not be treated as an
// opaque pair: m_dwIndex is at offset 0 and the UI-visible m_iFlagID is at
// offset 4.
type KinFlagID [KinFlagIDSize]byte

const (
	KinFlagIDSize = 8
	// NoKinFlagID is the native UI sentinel for "do not render a kin badge".
	// Zero is a valid badge ID and renders the blue placeholder frame.
	NoKinFlagID uint32 = 0xffffffff
)

func NewKinFlagID(index, flagID uint32) KinFlagID {
	var value KinFlagID
	binary.BigEndian.PutUint32(value[0:4], index)
	binary.BigEndian.PutUint32(value[4:8], flagID)
	return value
}

func (value KinFlagID) Index() uint32 {
	return binary.BigEndian.Uint32(value[0:4])
}

func (value KinFlagID) FlagID() uint32 {
	return binary.BigEndian.Uint32(value[4:8])
}

// KinFlagIDForWire converts the durable no-kin representation into the
// legacy client's UI sentinel. Persistence deliberately remains zero-valued;
// only protocol projections use -1 when KinIndex says the player has no kin.
func KinFlagIDForWire(kinIndex uint32, value KinFlagID) KinFlagID {
	if kinIndex == 0 {
		return NewKinFlagID(0, NoKinFlagID)
	}
	return value
}
