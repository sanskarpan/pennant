package snapshot

import (
	"encoding/json"

	"pennant/internal/model"
)

// ComputeDelta computes a delta from snapshot from to snapshot to.
// Returns nil if the delta exceeds 30% of the full snapshot size, signalling
// that the caller should send a full "put" instead of a patch.
func ComputeDelta(from, to *Snapshot) *Delta {
	delta := &Delta{
		EnvironmentKey: to.EnvironmentKey,
		ProjectKey:     to.ProjectKey,
		FromVersion:    from.Version,
		ToVersion:      to.Version,
		Checksum:       to.Checksum,
	}

	// Flags: upserted (new or changed)
	for key, toFlag := range to.Flags {
		fromFlag, existed := from.Flags[key]
		if !existed {
			delta.UpsertedFlags = append(delta.UpsertedFlags, toFlag)
			continue
		}
		fb, _ := json.Marshal(fromFlag)
		tb, _ := json.Marshal(toFlag)
		if string(fb) != string(tb) {
			delta.UpsertedFlags = append(delta.UpsertedFlags, toFlag)
		}
	}
	// Flags: deleted
	for key := range from.Flags {
		if _, exists := to.Flags[key]; !exists {
			delta.DeletedFlags = append(delta.DeletedFlags, key)
		}
	}

	// Segments: upserted (new or changed)
	for key, toSeg := range to.Segments {
		fromSeg, existed := from.Segments[key]
		if !existed {
			delta.UpsertedSegments = append(delta.UpsertedSegments, toSeg)
			continue
		}
		fb, _ := json.Marshal(fromSeg)
		tb, _ := json.Marshal(toSeg)
		if string(fb) != string(tb) {
			delta.UpsertedSegments = append(delta.UpsertedSegments, toSeg)
		}
	}
	// Segments: deleted
	for key := range from.Segments {
		if _, exists := to.Segments[key]; !exists {
			delta.DeletedSegments = append(delta.DeletedSegments, key)
		}
	}

	// If the delta is larger than 30% of the full snapshot, prefer a full put.
	deltaBytes, _ := json.Marshal(delta)
	snapBytes, _ := json.Marshal(to)
	if len(snapBytes) > 0 && float64(len(deltaBytes))/float64(len(snapBytes)) > 0.30 {
		return nil
	}
	return delta
}

// ApplyDelta applies a delta to a base snapshot and returns the resulting snapshot.
// It does not recompute the checksum — it trusts the delta's Checksum field, which
// was set from the target snapshot when ComputeDelta was called.
func ApplyDelta(base *Snapshot, delta *Delta) *Snapshot {
	next := &Snapshot{
		EnvironmentKey: base.EnvironmentKey,
		ProjectKey:     base.ProjectKey,
		Version:        delta.ToVersion,
		Flags:          make(map[string]*ResolvedFlag, len(base.Flags)),
		Segments:       make(map[string]*model.Segment, len(base.Segments)),
		PublishedAt:    base.PublishedAt,
		Checksum:       delta.Checksum,
	}

	// Copy existing flags then apply upserts and deletes.
	for k, v := range base.Flags {
		next.Flags[k] = v
	}
	for _, f := range delta.UpsertedFlags {
		next.Flags[f.Key] = f
	}
	for _, k := range delta.DeletedFlags {
		delete(next.Flags, k)
	}

	// Copy existing segments then apply upserts and deletes.
	for k, v := range base.Segments {
		next.Segments[k] = v
	}
	for _, seg := range delta.UpsertedSegments {
		next.Segments[seg.Key] = seg
	}
	for _, k := range delta.DeletedSegments {
		delete(next.Segments, k)
	}

	return next
}
