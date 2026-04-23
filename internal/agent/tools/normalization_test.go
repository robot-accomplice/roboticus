package tools

import "testing"

func TestNormalizationFactory_NormalizeToolCall_Identity(t *testing.T) {
	f := NewNormalizationFactory()
	got := f.NormalizeToolCall(ToolCallNormalizationInput{
		ToolName:     "query_table",
		RawArguments: `{"table":"sessions"}`,
	})
	if got.Disposition != NormalizationNoTransformNeeded {
		t.Fatalf("disposition = %s", got.Disposition)
	}
	if got.Transformer != "identity_structured_args" {
		t.Fatalf("transformer = %s", got.Transformer)
	}
}

func TestNormalizationFactory_NormalizeToolCall_QuotedStructuredJSON(t *testing.T) {
	f := NewNormalizationFactory()
	got := f.NormalizeToolCall(ToolCallNormalizationInput{
		ToolName:     "query_table",
		RawArguments: `"{\"table\":\"sessions\"}"`,
	})
	if got.Disposition != NormalizationQualifiedTransform {
		t.Fatalf("disposition = %s", got.Disposition)
	}
	if got.Arguments != `{"table":"sessions"}` {
		t.Fatalf("arguments = %q", got.Arguments)
	}
}

func TestNormalizationFactory_NormalizeToolCall_EmbeddedJSONObject(t *testing.T) {
	f := NewNormalizationFactory()
	got := f.NormalizeToolCall(ToolCallNormalizationInput{
		ToolName:     "query_table",
		RawArguments: `{"table": "sessions, "filters": {}{"table": "sessions", "filters": {}}`,
	})
	if got.Disposition != NormalizationQualifiedTransform {
		t.Fatalf("disposition = %s", got.Disposition)
	}
	if got.Transformer != "embedded_json_object" {
		t.Fatalf("transformer = %s", got.Transformer)
	}
	if got.Arguments != `{"table": "sessions", "filters": {}}` {
		t.Fatalf("arguments = %q", got.Arguments)
	}
}

func TestNormalizationFactory_NormalizeToolCall_NoQualifiedTransformer(t *testing.T) {
	f := NewNormalizationFactory()
	got := f.NormalizeToolCall(ToolCallNormalizationInput{
		ToolName:     "query_table",
		RawArguments: `table=sessions limit=10`,
	})
	if got.Disposition != NormalizationNoQualifiedTransformer {
		t.Fatalf("disposition = %s", got.Disposition)
	}
}

func TestNormalizationFactory_NormalizeToolResult_FilteredText(t *testing.T) {
	f := NewNormalizationFactory()
	got := f.NormalizeToolResult(ToolResultNormalizationInput{
		ToolName: "bash",
		Result: &Result{
			Output: "line one\nline one\n█████ 92%\nline two\n",
			Source: "builtin",
		},
	})
	if got.Disposition != NormalizationQualifiedTransform {
		t.Fatalf("disposition = %s", got.Disposition)
	}
	if got.Result == nil {
		t.Fatal("expected normalized result")
	}
	if got.Result.Output != "line one\nline two" {
		t.Fatalf("output = %q", got.Result.Output)
	}
}
