package py

import (
	"encoding/json"
	"testing"
)

func BenchmarkDispatchDef(b *testing.B) {
	br := &Bridge{}
	msg := map[string]json.RawMessage{
		"name":    json.RawMessage(`"test_trigger"`),
		"pattern": json.RawMessage(`"^hello (\\w+)$"`),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		br.dispatch("def", msg)
	}
}
