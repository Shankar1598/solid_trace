package ingest

import (
	"testing"
)

// ---------------------------------------------------------------------------
// classifyIssue — integration of all sub-functions
// ---------------------------------------------------------------------------

func TestClassifyIssue_BasicException(t *testing.T) {
	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "ZeroDivisionError",
					"value": "divided by 0",
				},
			},
		},
	}

	result := classifyIssue(payload)

	assertEqual(t, "title", "ZeroDivisionError: divided by 0", result.title)
	assertEqual(t, "kind", "error", result.kind)
	if result.fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
}

func TestClassifyIssue_MessageFallback(t *testing.T) {
	payload := map[string]interface{}{
		"message": "Something went wrong",
	}

	result := classifyIssue(payload)

	assertEqual(t, "title", "Something went wrong", result.title)
	assertEqual(t, "kind", "default", result.kind)
}

func TestClassifyIssue_CustomFingerprint(t *testing.T) {
	payload := map[string]interface{}{
		"message":     "Something went wrong",
		"fingerprint": []interface{}{"custom-group"},
	}

	result := classifyIssue(payload)

	// Custom fingerprint should differ from default
	defaultResult := classifyIssue(map[string]interface{}{
		"message": "Something went wrong",
	})

	if result.fingerprint == defaultResult.fingerprint {
		t.Errorf("custom fingerprint should differ from default; both are %s", result.fingerprint)
	}
}

func TestClassifyIssue_CustomFingerprintWithDefault(t *testing.T) {
	payload := map[string]interface{}{
		"message":     "Something went wrong",
		"fingerprint": []interface{}{"{{ default }}", "extra-salt"},
	}

	result := classifyIssue(payload)

	// Should include the default fingerprint components AND the extra salt
	defaultResult := classifyIssue(map[string]interface{}{
		"message": "Something went wrong",
	})

	if result.fingerprint == defaultResult.fingerprint {
		t.Errorf("fingerprint with extra salt should differ from pure default")
	}
}

// ---------------------------------------------------------------------------
// extractTitle
// ---------------------------------------------------------------------------

func TestExtractTitle_ExceptionMapWithValues(t *testing.T) {
	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "RuntimeError",
					"value": "something broke",
				},
			},
		},
	}

	got := extractTitle(payload)
	assertEqual(t, "title", "RuntimeError: something broke", got)
}

func TestExtractTitle_ExceptionArray(t *testing.T) {
	// Some SDKs send exception as a bare array (not wrapped in {values: [...]})
	payload := map[string]interface{}{
		"exception": []interface{}{
			map[string]interface{}{
				"type":  "TypeError",
				"value": "undefined is not a function",
			},
		},
	}

	got := extractTitle(payload)
	assertEqual(t, "title", "TypeError: undefined is not a function", got)
}

func TestExtractTitle_MultipleExceptionsUsesLast(t *testing.T) {
	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "CauseError",
					"value": "root cause",
				},
				map[string]interface{}{
					"type":  "WrapperError",
					"value": "top-level error",
				},
			},
		},
	}

	got := extractTitle(payload)
	assertEqual(t, "title", "WrapperError: top-level error", got)
}

func TestExtractTitle_TypeOnly(t *testing.T) {
	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type": "NullPointerException",
				},
			},
		},
	}

	got := extractTitle(payload)
	assertEqual(t, "title", "NullPointerException", got)
}

func TestExtractTitle_ValueOnly(t *testing.T) {
	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"value": "something happened",
				},
			},
		},
	}

	got := extractTitle(payload)
	assertEqual(t, "title", "something happened", got)
}

func TestExtractTitle_MultilineValueTruncatedToFirstLine(t *testing.T) {
	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "Error",
					"value": "first line\nsecond line\nthird line",
				},
			},
		},
	}

	got := extractTitle(payload)
	assertEqual(t, "title", "Error: first line", got)
}

func TestExtractTitle_MessageFallback(t *testing.T) {
	payload := map[string]interface{}{
		"message": "A log message",
	}

	got := extractTitle(payload)
	assertEqual(t, "title", "A log message", got)
}

func TestExtractTitle_NoExceptionNoMessage(t *testing.T) {
	payload := map[string]interface{}{
		"level": "error",
	}

	got := extractTitle(payload)
	assertEqual(t, "title", "Unknown Error", got)
}

func TestExtractTitle_EmptyExceptionValues(t *testing.T) {
	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{},
		},
		"message": "fallback message",
	}

	got := extractTitle(payload)
	assertEqual(t, "title", "fallback message", got)
}

func TestExtractTitle_TruncatesLongTitle(t *testing.T) {
	longValue := make([]byte, 300)
	for i := range longValue {
		longValue[i] = 'a'
	}

	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "E",
					"value": string(longValue),
				},
			},
		},
	}

	got := extractTitle(payload)
	if len(got) > 250 {
		t.Errorf("expected title to be truncated to 250 chars, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// extractCulprit
// ---------------------------------------------------------------------------

func TestExtractCulprit_ExplicitCulprit(t *testing.T) {
	payload := map[string]interface{}{
		"culprit": "app/controllers/users_controller.rb:42",
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "app/controllers/users_controller.rb:42", got)
}

func TestExtractCulprit_TransactionFallback(t *testing.T) {
	payload := map[string]interface{}{
		"transaction": "GET /api/users",
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "GET /api/users", got)
}

func TestExtractCulprit_StacktraceGenericPlatform(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "ruby",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "RuntimeError",
					"value": "boom",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "lib/external.rb",
								"function": "do_stuff",
								"lineno":   float64(10),
								"in_app":   false,
							},
							map[string]interface{}{
								"filename": "app/models/user.rb",
								"function": "save!",
								"lineno":   float64(42),
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "save! in app/models/user.rb:42", got)
}

func TestExtractCulprit_StacktraceJavaScriptPlatform(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "javascript",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "TypeError",
					"value": "undefined is not a function",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "app.js",
								"function": "handleClick",
								"lineno":   float64(55),
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "handleClick(app.js:55)", got)
}

func TestExtractCulprit_StacktraceNodePlatform(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "node",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "Error",
					"value": "ENOENT",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "src/server.ts",
								"function": "readFile",
								"lineno":   float64(100),
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "readFile(src/server.ts:100)", got)
}

func TestExtractCulprit_StacktraceObjCPlatform(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "objc",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "NSException",
					"value": "out of bounds",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"function": "-[AppDelegate applicationDidFinishLaunching:]",
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "-[AppDelegate applicationDidFinishLaunching:]", got)
}

func TestExtractCulprit_StacktraceCocoaPlatform(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "cocoa",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "NSException",
					"value": "crash",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"function": "swift_crash",
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "swift_crash", got)
}

func TestExtractCulprit_StacktraceNativePlatform(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "native",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "SIGSEGV",
					"value": "segfault",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"function": "main_loop",
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "main_loop", got)
}

func TestExtractCulprit_FramePlatformOverridesEventPlatform(t *testing.T) {
	// Frame-level platform takes precedence over event-level
	payload := map[string]interface{}{
		"platform": "ruby",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "Error",
					"value": "boom",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "app.js",
								"function": "onClick",
								"lineno":   float64(10),
								"platform": "javascript",
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	// Should use JS format because the frame overrides platform
	assertEqual(t, "culprit", "onClick(app.js:10)", got)
}

func TestExtractCulprit_PrefersInAppFrame(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "ruby",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "Error",
					"value": "boom",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "lib/gem.rb",
								"function": "gem_method",
								"lineno":   float64(5),
								"in_app":   false,
							},
							map[string]interface{}{
								"filename": "app/models/user.rb",
								"function": "validate",
								"lineno":   float64(20),
								"in_app":   true,
							},
							map[string]interface{}{
								"filename": "lib/other_gem.rb",
								"function": "other",
								"lineno":   float64(99),
								"in_app":   false,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	// Should pick the in_app frame (scanning from end), which is "validate"
	assertEqual(t, "culprit", "validate in app/models/user.rb:20", got)
}

func TestExtractCulprit_FallsBackToNonInAppFrame(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "ruby",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "Error",
					"value": "boom",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "lib/gem.rb",
								"function": "gem_method",
								"lineno":   float64(5),
								"in_app":   false,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "gem_method in lib/gem.rb:5", got)
}

func TestExtractCulprit_SyntheticMechanismReturnsEmpty(t *testing.T) {
	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "Error",
					"value": "boom",
					"mechanism": map[string]interface{}{
						"synthetic": true,
					},
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "app.rb",
								"function": "run",
								"lineno":   float64(1),
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "", got)
}

func TestExtractCulprit_TopLevelStacktrace(t *testing.T) {
	// Some SDKs put stacktrace at the top level, not inside exceptions
	payload := map[string]interface{}{
		"platform": "ruby",
		"stacktrace": map[string]interface{}{
			"frames": []interface{}{
				map[string]interface{}{
					"filename": "script.rb",
					"function": "main",
					"lineno":   float64(1),
					"in_app":   true,
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "main in script.rb:1", got)
}

func TestExtractCulprit_RequestURLFallback(t *testing.T) {
	payload := map[string]interface{}{
		"request": map[string]interface{}{
			"url": "https://example.com/api/users",
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "https://example.com/api/users", got)
}

func TestExtractCulprit_ModuleFallbackWhenNoFilename(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "python",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "ValueError",
					"value": "bad value",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"module":   "myapp.views",
								"function": "handle_request",
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "handle_request in myapp.views", got)
}

func TestExtractCulprit_JSNoFunction(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "javascript",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "Error",
					"value": "boom",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "bundle.js",
								"lineno":   float64(1),
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "?(bundle.js:1)", got)
}

func TestExtractCulprit_GenericNoFunction(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "ruby",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "Error",
					"value": "boom",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "app.rb",
								"lineno":   float64(5),
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "? in app.rb:5", got)
}

func TestExtractCulprit_Empty(t *testing.T) {
	payload := map[string]interface{}{}

	got := extractCulprit(payload)
	assertEqual(t, "culprit", "", got)
}

// ---------------------------------------------------------------------------
// determineKind
// ---------------------------------------------------------------------------

func TestDetermineKind_CSPReport(t *testing.T) {
	payload := map[string]interface{}{
		"csp-report": map[string]interface{}{},
	}

	got := determineKind(payload)
	assertEqual(t, "kind", "csp", got)
}

func TestDetermineKind_CSPLogger(t *testing.T) {
	payload := map[string]interface{}{
		"logger": "csp",
	}

	got := determineKind(payload)
	assertEqual(t, "kind", "csp", got)
}

func TestDetermineKind_Exception(t *testing.T) {
	payload := map[string]interface{}{
		"exception": map[string]interface{}{
			"values": []interface{}{},
		},
	}

	got := determineKind(payload)
	assertEqual(t, "kind", "error", got)
}

func TestDetermineKind_Default(t *testing.T) {
	payload := map[string]interface{}{
		"message": "hello",
	}

	got := determineKind(payload)
	assertEqual(t, "kind", "default", got)
}

func TestDetermineKind_CSPReportTakesPrecedence(t *testing.T) {
	// If both csp-report and exception exist, CSP wins
	payload := map[string]interface{}{
		"csp-report": map[string]interface{}{},
		"exception": map[string]interface{}{
			"values": []interface{}{},
		},
	}

	got := determineKind(payload)
	assertEqual(t, "kind", "csp", got)
}

// ---------------------------------------------------------------------------
// computeFingerprint
// ---------------------------------------------------------------------------

func TestComputeFingerprint_DefaultIsStable(t *testing.T) {
	fp1 := computeFingerprint("Error: boom", "app.rb:42", "error", nil)
	fp2 := computeFingerprint("Error: boom", "app.rb:42", "error", nil)

	if fp1 != fp2 {
		t.Errorf("same inputs should produce same fingerprint; got %s and %s", fp1, fp2)
	}
	if fp1 == "" {
		t.Error("fingerprint should not be empty")
	}
}

func TestComputeFingerprint_DifferentInputsDiffer(t *testing.T) {
	fp1 := computeFingerprint("Error: boom", "app.rb:42", "error", nil)
	fp2 := computeFingerprint("Error: crash", "app.rb:42", "error", nil)

	if fp1 == fp2 {
		t.Error("different titles should produce different fingerprints")
	}
}

func TestComputeFingerprint_CustomOverridesDefault(t *testing.T) {
	defaultFP := computeFingerprint("Error: boom", "app.rb:42", "error", nil)
	customFP := computeFingerprint("Error: boom", "app.rb:42", "error", []string{"my-custom-group"})

	if defaultFP == customFP {
		t.Error("custom fingerprint should differ from default")
	}
}

func TestComputeFingerprint_CustomWithDefaultExpansion(t *testing.T) {
	defaultFP := computeFingerprint("Error: boom", "app.rb:42", "error", nil)
	expandedFP := computeFingerprint("Error: boom", "app.rb:42", "error", []string{"{{ default }}"})

	// {{ default }} alone should produce the same as no custom fingerprint
	// because it expands to "title||culprit||kind" which is the same as the default format
	if defaultFP != expandedFP {
		t.Errorf("{{ default }} alone should equal default fingerprint; got %s vs %s", defaultFP, expandedFP)
	}
}

func TestComputeFingerprint_CustomWithDefaultPlusSalt(t *testing.T) {
	defaultFP := computeFingerprint("Error: boom", "app.rb:42", "error", nil)
	saltedFP := computeFingerprint("Error: boom", "app.rb:42", "error", []string{"{{ default }}", "extra-salt"})

	if defaultFP == saltedFP {
		t.Error("{{ default }} with extra salt should differ from default")
	}
}

func TestComputeFingerprint_MultipleCustomParts(t *testing.T) {
	fp := computeFingerprint("Error: boom", "app.rb:42", "error", []string{"group-a", "group-b"})

	if fp == "" {
		t.Error("fingerprint should not be empty")
	}

	// Same parts should be stable
	fp2 := computeFingerprint("Error: boom", "app.rb:42", "error", []string{"group-a", "group-b"})
	if fp != fp2 {
		t.Errorf("same custom parts should produce same fingerprint; got %s vs %s", fp, fp2)
	}
}

// ---------------------------------------------------------------------------
// extractCustomFingerprint
// ---------------------------------------------------------------------------

func TestExtractCustomFingerprint_NotPresent(t *testing.T) {
	payload := map[string]interface{}{}
	got := extractCustomFingerprint(payload)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestExtractCustomFingerprint_ArrayOfStrings(t *testing.T) {
	payload := map[string]interface{}{
		"fingerprint": []interface{}{"part-a", "part-b"},
	}
	got := extractCustomFingerprint(payload)
	if len(got) != 2 || got[0] != "part-a" || got[1] != "part-b" {
		t.Errorf("expected [part-a, part-b], got %v", got)
	}
}

func TestExtractCustomFingerprint_SkipsNonStrings(t *testing.T) {
	payload := map[string]interface{}{
		"fingerprint": []interface{}{"valid", float64(42), "also-valid"},
	}
	got := extractCustomFingerprint(payload)
	if len(got) != 2 || got[0] != "valid" || got[1] != "also-valid" {
		t.Errorf("expected [valid, also-valid], got %v", got)
	}
}

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate_ShortString(t *testing.T) {
	got := truncate("hello", 10)
	assertEqual(t, "truncate", "hello", got)
}

func TestTruncate_ExactLength(t *testing.T) {
	got := truncate("hello", 5)
	assertEqual(t, "truncate", "hello", got)
}

func TestTruncate_LongString(t *testing.T) {
	got := truncate("hello world", 5)
	assertEqual(t, "truncate", "hello", got)
}

// ---------------------------------------------------------------------------
// Multiple exceptions — culprit uses last exception's stacktrace
// ---------------------------------------------------------------------------

func TestExtractCulprit_MultipleExceptionsUsesLastStacktrace(t *testing.T) {
	payload := map[string]interface{}{
		"platform": "ruby",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "CauseError",
					"value": "root cause",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "lib/cause.rb",
								"function": "cause_method",
								"lineno":   float64(1),
								"in_app":   true,
							},
						},
					},
				},
				map[string]interface{}{
					"type":  "WrapperError",
					"value": "wrapper",
					"stacktrace": map[string]interface{}{
						"frames": []interface{}{
							map[string]interface{}{
								"filename": "app/wrapper.rb",
								"function": "wrap",
								"lineno":   float64(10),
								"in_app":   true,
							},
						},
					},
				},
			},
		},
	}

	got := extractCulprit(payload)
	// Should use the last stacktrace (WrapperError's)
	assertEqual(t, "culprit", "wrap in app/wrapper.rb:10", got)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertEqual(t *testing.T, field, expected, actual string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %q, got %q", field, expected, actual)
	}
}
