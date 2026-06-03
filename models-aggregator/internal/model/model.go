// Package model holds the aggregator's domain types. It has no external dependencies, so the core
// of the service can be reasoned about and unit-tested without Kubernetes or HTTP.
package model

// Object is a single OpenAI "model" object. It is kept as a free-form map so every field a backend
// reports (created, owned_by, max_model_len, permission, root, parent, …) survives pass-through;
// only the id is ever rewritten.
type Object map[string]any

// ID returns the object's "id" field, or "" if absent/not a string.
func (o Object) ID() string {
	s, _ := o["id"].(string)
	return s
}

// SetID overwrites the object's "id" field.
func (o Object) SetID(id string) { o["id"] = id }

// Target is a discovered model backend to poll. Key is unique per model and used as the cache key.
// Rewrite maps the objects a backend reports to the objects advertised on /v1/models (id rewriting
// + which entries to keep); it travels with the target so callers need no per-source switch.
type Target struct {
	Key     string // cache key — KServe: the FQN prefix; SLURM: the route's fixed FQN
	Backend string // host:port to probe, or "" if unknown
	BaseID  string // advertised FQN of the base model (used by the fallback entry)
	Root    string // real served-model name the backend answers to
	OwnedBy string // owner label for the fallback entry ("kserve" | "slurm")
	Created int64  // creation timestamp for the fallback entry
	Rewrite func([]Object) []Object
}

// Fallback is the minimal entry advertised for a target whose backend has never been reached, so a
// known model never silently disappears from the listing.
func (t Target) Fallback() Object {
	return Object{
		"id":       t.BaseID,
		"object":   "model",
		"created":  t.Created,
		"owned_by": t.OwnedBy,
		"root":     t.Root,
	}
}

// PrefixRewriter prepends prefix to every reported object's id. Used for KServe backends, where the
// base model and any LoRA adapters each become a routable fully-qualified name. Objects without an
// id are dropped.
func PrefixRewriter(prefix string) func([]Object) []Object {
	return func(in []Object) []Object {
		out := make([]Object, 0, len(in))
		for _, o := range in {
			if o.ID() == "" {
				continue
			}
			o.SetID(prefix + o.ID())
			out = append(out, o)
		}
		return out
	}
}

// FixedRewriter advertises exactly one routable id (fixedID) for a target. Used for SLURM backends,
// where a single HTTPRoute maps one header value to the backend: it keeps the object whose id
// matches root (else the first) and stamps it with fixedID; adapters are not separately routable.
func FixedRewriter(fixedID, root string) func([]Object) []Object {
	return func(in []Object) []Object {
		base := pickBase(in, root)
		if base == nil {
			return nil
		}
		base.SetID(fixedID)
		return []Object{base}
	}
}

// pickBase returns the object whose id equals root, or the first object if none match.
func pickBase(in []Object, root string) Object {
	for _, o := range in {
		if o.ID() == root {
			return o
		}
	}
	if len(in) > 0 {
		return in[0]
	}
	return nil
}
