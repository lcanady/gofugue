package script

import (
	"fmt"
	"regexp"

	lru "github.com/hashicorp/golang-lru/v2"
)

var reCache *lru.Cache[string, *regexp.Regexp]

func init() {
	var err error
	reCache, err = lru.New[string, *regexp.Regexp](128)
	if err != nil {
		panic(fmt.Sprintf("failed to initialize script regex lru cache: %v", err))
	}
}

// CompileRegexp is a cached wrapper around regexp.Compile.
func CompileRegexp(pattern string) (*regexp.Regexp, error) {
	if cachedRe, ok := reCache.Get(pattern); ok {
		return cachedRe, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	reCache.Add(pattern, re)
	return re, nil
}
