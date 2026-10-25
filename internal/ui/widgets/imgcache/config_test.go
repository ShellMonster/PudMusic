package imgcache

import "testing"

func TestImageCacheBudgetFromEnvironment(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int64
	}{
		{"", 32 << 20}, {"96", 96 << 20}, {"32", 32 << 20}, {"16", 16 << 20},
		{" 32 ", 32 << 20}, {"1", 1 << 20}, {"4096", 4096 << 20},
		{"0", DefaultMaxBytes}, {"-1", DefaultMaxBytes}, {"4097", DefaultMaxBytes},
		{"999999999999999999999", DefaultMaxBytes}, {"NaN", DefaultMaxBytes}, {"1.5", DefaultMaxBytes},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("BIU_IMAGE_CACHE_MB", tc.value)
			c := New(Options{})
			defer c.Close()
			if c.lru.max != tc.want {
				t.Fatalf("budget = %d, want %d", c.lru.max, tc.want)
			}
			for _, reset := range []int64{0, -1} {
				c.SetMaxBytes(8 << 20)
				c.SetMaxBytes(reset)
				if c.lru.max != tc.want {
					t.Fatalf("reset(%d) budget = %d, want %d", reset, c.lru.max, tc.want)
				}
			}
		})
	}
	t.Setenv("BIU_IMAGE_CACHE_MB", "16")
	c := New(Options{MaxBytes: 32 << 20})
	defer c.Close()
	if c.lru.max != 32<<20 {
		t.Fatal("explicit budget must override the environment")
	}
	c.SetMaxBytes(64 << 20)
	if c.lru.max != 64<<20 {
		t.Fatal("explicit runtime budget must override the environment")
	}
	c.SetMaxBytes(0)
	if c.lru.max != 16<<20 {
		t.Fatal("reset must restore the configured default, not the compiled default")
	}
}
