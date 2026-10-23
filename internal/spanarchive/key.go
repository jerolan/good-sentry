package spanarchive

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

// CohortKey identifies a fixed project and set of immutable span row IDs. The
// same cohort maps to the same object key regardless of query row order.
func CohortKey(projectID int64, rowIDs []int64) (string, error) {
	if projectID <= 0 || len(rowIDs) == 0 || len(rowIDs) > MaxRows {
		return "", fmt.Errorf("span archive key requires a positive project ID and 1 to %d row IDs", MaxRows)
	}
	ordered := append([]int64(nil), rowIDs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	hash := sha256.New()
	hash.Write([]byte("goodsentry-span-archive-v1\x00"))
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(projectID))
	hash.Write(encoded[:])
	for i, rowID := range ordered {
		if rowID <= 0 || i > 0 && ordered[i-1] == rowID {
			return "", errors.New("span archive key requires unique positive row IDs")
		}
		binary.BigEndian.PutUint64(encoded[:], uint64(rowID))
		hash.Write(encoded[:])
	}
	return fmt.Sprintf("projects/%d/spans/%s.parquet", projectID, hex.EncodeToString(hash.Sum(nil))), nil
}
