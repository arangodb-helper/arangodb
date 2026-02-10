package agency

// Key represents an Agency key path
type Key []string

func (k Key) CreateSubKey(parts ...string) Key {
	out := make(Key, 0, len(k)+len(parts))
	out = append(out, k...)
	out = append(out, parts...)
	return out
}
