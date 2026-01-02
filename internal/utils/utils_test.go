package utils

import (
	"testing"
)

func TestMaskUsername(t *testing.T) {
	testCases := map[string]struct {
		input    string
		expected string
	}{
		"test01": {"ed@gmail.com", "**@gmail.com"},
		"test02": {"bob@yahoo.com", "b**@yahoo.com"},
		"test03": {"rick@icloud.com", "r**k@icloud.com"},
		"test04": {"golang.community@gmail.com", "g**************y@gmail.com"},
		"test05": {"notTheEmailFormat", "n***************t"},
		"test06": {"", ""},
	}

	for testName, testData := range testCases {
		t.Run(testName, func(t *testing.T) {
			result := MaskUsername(testData.input)
			if result != testData.expected {
				t.Errorf(`MaskUsername("%+v") returned '%+v', expected '%+v'`, testData.input, result, testData.expected)
			}
		})
	}
}

func TestIntFromString(t *testing.T) {
	testCases := map[string]struct {
		testString string
		expected   int
	}{
		"test01": {"Alice got 8 pieces of pizza", 8},
		"test02": {"Alice gave $15 to Bob", 15},
		"test03": {"We got 5 files and 8 more", 58},
		"test04": {"Rank is 3,210 now", 3210},
		"test05": {"Your credit is -3,456.75", -3456},
	}

	for testName, testData := range testCases {
		t.Run(testName, func(t *testing.T) {
			result, err := intFromString(testData.testString)
			if err != nil {
				t.Errorf(`IntFromString("%+v") returned unexpected error: %+v`, testData.testString, err)
			}

			if result != testData.expected {
				t.Errorf(`IntToString("%+v") returned '%+v', expected '%+v'`, testData.testString, result, testData.expected)
			}
		})
	}
}

func TestFloatFromString(t *testing.T) {
	testCases := map[string]struct {
		testString string
		expected   float64
	}{
		"test01": {"Alice got 8 pieces of pizza", 8.0},
		"test02": {"Alice gave $15.67 to Bob", 15.67},
		"test03": {"We got 5 files and 8.1 more", 58.1},
		"test04": {"Rank is 3,210.12 now", 3210.12},
		"test05": {"Your credit is -3,456.78", -3456.78},
	}

	for testName, testData := range testCases {
		t.Run(testName, func(t *testing.T) {
			result, err := floatFromString(testData.testString)
			if err != nil {
				t.Errorf(`floatFromString("%+v") returned unexpected error: %+v`, testData.testString, err)
			}

			if result != testData.expected {
				t.Errorf(`floatFromString("%+v") returned '%+v', expected '%+v'`, testData.testString, result, testData.expected)
			}
		})
	}
}

func TestParseDurationStringToSeconds(t *testing.T) {
	testCases := map[string]struct {
		testString string
		expected   int
	}{
		"test01": {"00:00:15", 15},
		"test02": {"00:30:18", 1818},
		"test03": {"01:45:22", 6322},
		"test04": {"10:00:00", 36000},
		"test05": {"04:20:00", 15600},
		"test06": {"23:00:05", 82805},
	}
	for testName, testData := range testCases {
		t.Run(testName, func(t *testing.T) {
			result, err := ParseDurationStringToSeconds(testData.testString)
			if err != nil {
				t.Errorf(`ParseDurationStringToSeconds("%+v") returned unexpected error: %+v`, testData.testString, err)
			}

			if result != testData.expected {
				t.Errorf(`ParseDurationStringToSeconds("%+v") returned '%+v', expected '%+v'`, testData.testString, result, testData.expected)
			}
		})
	}
}
