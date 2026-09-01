package daemon

import "unicode/utf16"

// parseHDROPFilePaths extracts the path list from the bytes of a CF_HDROP
// clipboard payload. The block begins with a DROPFILES header whose pFiles
// field (a UINT at byte offset 0) is the byte offset to the first path; the
// paths then form a UTF-16 double-NUL-terminated array. It is a pure function
// so it can be unit-tested without a real clipboard; the Windows clipboard
// reader passes the raw global-memory bytes here. An unparseable payload
// returns nil rather than panicking.
func parseHDROPFilePaths(raw []byte) []string {
	if len(raw) < 4 {
		return nil
	}
	start := int(uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16 | uint32(raw[3])<<24)
	if start < 0 || start >= len(raw) {
		return nil
	}

	var paths []string
	for i := start; i+1 < len(raw); {
		byteStart := i
		for i+1 < len(raw) {
			u := uint16(raw[i]) | uint16(raw[i+1])<<8
			if u == 0 {
				break
			}
			i += 2
		}
		if i == byteStart {
			// A NUL unit immediately at the list position ends the array.
			break
		}
		unit := make([]uint16, (i-byteStart)/2)
		for k := range unit {
			unit[k] = uint16(raw[byteStart+2*k]) | uint16(raw[byteStart+2*k+1])<<8
		}
		paths = append(paths, string(utf16.Decode(unit)))
		i += 2 // skip the terminating NUL unit
	}
	return paths
}
