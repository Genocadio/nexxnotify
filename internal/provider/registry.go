package provider

type Registry struct {
	providers []Provider
	defaults  map[Channel]string
}

func NewRegistry(providers []Provider, overrides map[Channel]string) *Registry {
	return &Registry{
		providers: providers,
		defaults:  resolveDefaults(providers, overrides),
	}
}

func resolveDefaults(providers []Provider, overrides map[Channel]string) map[Channel]string {
	defaults := make(map[Channel]string)
	for _, ch := range AllChannels {
		override, hasOverride := overrides[ch]
		for _, p := range providers {
			st, ok := StatusFor(p, ch)
			if !ok || !st.Configured {
				continue
			}
			if hasOverride && override != p.Name() {
				continue
			}
			defaults[ch] = p.Name()
			break
		}
	}
	return defaults
}

func (r *Registry) Providers() []Provider {
	return r.providers
}

func (r *Registry) ByName(name string) (Provider, bool) {
	for _, p := range r.providers {
		if p.Name() == name {
			return p, true
		}
	}
	return nil, false
}

func (r *Registry) Default(ch Channel) (string, bool) {
	name, ok := r.defaults[ch]
	return name, ok
}

func (r *Registry) IsDefault(name string, ch Channel) bool {
	d, ok := r.Default(ch)
	return ok && d == name
}
