package utils

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ashokhin/am4bot/internal/model"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/prometheus/client_golang/prometheus"
)

// MaskUsername is an anonymization function for logging user name in the logs.
func MaskUsername(userName string) string {
	userNameParts := strings.Split(userName, "@")

	// if it's not an email format then mask the whole string
	if len(userNameParts) == 1 {
		return maskString(userNameParts[0])
	}

	// mask only the user name part of the email
	return fmt.Sprintf("%s@%s", maskString(userNameParts[0]), userNameParts[1])
}

// maskString replaces part of the string with asterisks for anonymization.
func maskString(str string) string {
	switch {
	case len(str) == 3:
		return fmt.Sprintf("%s%s", string(str[0]), strings.Repeat("*", len(str)-1))

	case len(str) > 3:
		return fmt.Sprintf("%s%s%s", string(str[0]), strings.Repeat("*", len(str)-2), string(str[len(str)-1]))
	default:
		return strings.Repeat("*", len(str))
	}
}

// intFromString deletes all non-digit values like words, letters, signs, spaces etc. and returns Integer value.
func intFromString(str string) (int, error) {
	var intValue int
	var err error

	intString := strings.ReplaceAll(strings.Split(str, ".")[0], ",", "")
	allNumRegex := regexp.MustCompile(`(-)?(\d)+`)
	intString = strings.Join(allNumRegex.FindAllString(intString, -1), "")
	intValue, err = strconv.Atoi(intString)
	if err != nil {
		slog.Debug("error in utils.intFromString", "string", str, "error", err)

		return -1, err
	}

	return intValue, nil
}

// floatFromString deletes all non-digit values like words, letters, signs, spaces etc. and returns float value.
func floatFromString(str string) (float64, error) {
	var floatValue float64
	var err error

	floatString := strings.ReplaceAll(str, ",", "")
	allNumRegex := regexp.MustCompile(`(-)?(\d)+(\.\d+)?`)
	floatString = strings.Join(allNumRegex.FindAllString(floatString, -1), "")
	floatValue, err = strconv.ParseFloat(floatString, 64)
	if err != nil {
		slog.Warn("error in utils.floatFromString", "string", str, "error", err)

		return floatValue, err
	}

	return floatValue, nil
}

// getCallerFunctionName returns the name of the calling function.
func getCallerFunctionName() string {
	pc, _, _, _ := runtime.Caller(2)
	f := runtime.FuncForPC(pc)

	if f == nil {
		return ""
	}

	sliceOfFuncPath := strings.Split(f.Name(), ".")
	funcName := sliceOfFuncPath[len(sliceOfFuncPath)-1]

	return funcName
}

// RefreshPage reloads the current page and waits until the loading overlay is not visible.
func RefreshPage() chromedp.Tasks {
	slog.Debug("refresh page")

	return chromedp.Tasks{
		chromedp.Reload(),
		chromedp.WaitNotVisible(model.OVERLAY_LOADING, chromedp.ByQuery),
	}
}

// Screenshot takes a full-page screenshot and saves it to a file named with the caller function and timestamp.
func Screenshot() chromedp.Tasks {
	var buf []byte

	slog.Debug("take a screenshot")

	callerFunc := getCallerFunctionName()
	scrName := fmt.Sprintf("screenshot_%s_%s.png", callerFunc, time.Now().Format("2006-01-02T15-04-05"))

	slog.Debug("save screenshot", "file", scrName)

	return chromedp.Tasks{
		chromedp.FullScreenshot(&buf, 100),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := os.WriteFile(scrName, buf, 0644); err != nil {
				return err
			}

			return nil
		}),
	}
}

// GetIntFromElement is an element query action that retrieves the visible text of the first element
// node matching the selector and converts it to Integer.
func GetIntFromElement(sel string, resultInt *int) chromedp.Tasks {
	var resultStr string
	var err error

	slog.Debug("get integer from element", "element", sel)

	return chromedp.Tasks{
		chromedp.Text(sel, &resultStr, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			*resultInt, err = intFromString(resultStr)
			if err != nil {
				slog.Debug("error in utils.GetIntFromElement > utils.intFromString",
					"string", resultStr, "error", err)

				return err
			}

			slog.Debug("got integer from element", "value", *resultInt)

			return nil
		}),
	}
}

// GetIntFromChildElement is an element query action that retrieves the visible text of a child element
func GetIntFromChildElement(sel string, resultInt *int, node *cdp.Node) chromedp.Tasks {
	var resultStr string
	var err error

	slog.Debug("get integer from child element", "element", sel)

	return chromedp.Tasks{
		chromedp.Text(sel, &resultStr, chromedp.ByQuery, chromedp.FromNode(node)),
		chromedp.ActionFunc(func(ctx context.Context) error {
			*resultInt, err = intFromString(resultStr)
			if err != nil {
				slog.Debug("error in utils.GetIntFromElement > utils.intFromString",
					"string", resultStr, "error", err)

				return err
			}

			slog.Debug("got integer from child element", "value", *resultInt)

			return nil
		}),
	}
}

// GetIntFromChildElementAttribute retrieves the value of a specified attribute from a child element
func GetIntFromChildElementAttribute(sel string, resultInt *int, node *cdp.Node) error {
	var resultStr string
	var err error

	slog.Debug("get integer from child element attribute", "attribute", sel)

	resultStr = node.AttributeValue(sel)
	*resultInt, err = intFromString(resultStr)

	if err != nil {
		slog.Warn("error in utils.GetIntFromElement > utils.intFromString",
			"string", resultStr, "error", err)

		return err
	}

	slog.Debug("got integer from child element attribute", "value", *resultInt)

	return nil
}

// GetFloatFromElement is an element query action that retrieves the visible text of the first element
// node matching the selector and converts it to Float.
func GetFloatFromElement(sel string, resultFloat *float64) chromedp.Tasks {
	var resultStr string
	var err error

	slog.Debug("get float from element", "element", sel)

	return chromedp.Tasks{
		chromedp.Text(sel, &resultStr, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			*resultFloat, err = floatFromString(resultStr)
			if err != nil {
				slog.Warn("error in utils.GetFloatFromElement > utils.floatFromString",
					"string", resultStr, "error", err)

				return err
			}

			slog.Debug("got float from element", "value", *resultFloat)

			return nil
		}),
	}
}

// GetFloatFromChildElement is an element query action that retrieves the visible text of a child element
func GetFloatFromChildElement(sel string, resultFloat *float64, node *cdp.Node) chromedp.Tasks {
	var resultStr string
	var err error

	slog.Debug("get float from child element", "element", sel)

	return chromedp.Tasks{
		chromedp.Text(sel, &resultStr, chromedp.ByQuery, chromedp.FromNode(node)),
		chromedp.ActionFunc(func(ctx context.Context) error {
			*resultFloat, err = floatFromString(resultStr)
			if err != nil {
				slog.Warn("error in utils.GetFloatFromChildElement > utils.floatFromString",
					"string", resultStr, "error", err)

				return err
			}

			slog.Debug("got float from child element", "value", *resultFloat)

			return nil
		}),
	}
}

// GetFloatFromChildElementAttribute retrieves the value of a specified attribute from a child element
func GetFloatFromChildElementAttribute(sel string, resultFloat *float64, node *cdp.Node) error {
	var resultStr string
	var err error

	slog.Debug("get float from child element attribute", "attribute", sel)

	resultStr = node.AttributeValue(sel)
	*resultFloat, err = floatFromString(resultStr)

	if err != nil {
		slog.Warn("error in utils.GetIntFromElement > utils.floatFromString",
			"string", resultStr, "error", err)

		return err
	}

	slog.Debug("got float from child element attribute", "value", *resultFloat)

	return nil
}

// ClickElement sends a mouse click event to the first element matching the selector.
// It waits for 2 seconds after the click.
// This function returns chromedp.Tasks to be used in a chromedp.Run call.
func ClickElement(sel string) chromedp.Tasks {
	slog.Debug("click element", "element", sel)

	return chromedp.Tasks{
		chromedp.Click(sel, chromedp.ByQuery),
		chromedp.Sleep(2 * time.Second),
	}
}

// DoClickElement sends a mouse click event to the first element matching the selector.
// It waits for 2 seconds after the click.
// This function executes the click immediately using the provided context.
func DoClickElement(ctx context.Context, sel string) error {
	slog.Debug("click element", "element", sel)

	if err := chromedp.Run(ctx,
		chromedp.Click(sel, chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
	); err != nil {
		slog.Warn("error in utils.DoClickElement", "error", err)

		return err
	}

	return nil
}

// IsElementVisible checks if an element matching the selector is visible on the page.
func IsElementVisible(ctx context.Context, sel string, waitTimeoutArgs ...int) bool {
	slog.Debug("check if element is visible", "element", sel)

	// define default timeout
	waitTimeout := 2 * time.Second

	if len(waitTimeoutArgs) > 0 {
		waitTimeout = time.Duration(waitTimeoutArgs[0]) * time.Second
	}

	// create a local context with timeout
	ctx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()

	// wait for 2 seconds for the element to be visible
	// if element is not found then return false - element is not visible
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sel, chromedp.ByQuery),
	); err != nil {
		// if not found for the ctx timeout then return false - element is not visible
		slog.Debug("error in utils.IsElementVisible", "selector", sel, "error", err)

		return false
	}

	slog.Debug("element is visible", "selector", sel)

	return true
}

// IsSubElementVisible checks if a sub-element matching the selector is visible within a given node.
func IsSubElementVisible(ctx context.Context, sel string, node *cdp.Node) bool {
	var nodesList []*cdp.Node

	slog.Debug("check if sub-element is visible", "element", sel)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	slog.Debug("init nodesList", "len", len(nodesList))

	if err := chromedp.Run(ctx,
		chromedp.Nodes(sel, &nodesList, chromedp.ByQueryAll, chromedp.FromNode(node)),
	); err != nil {
		// if not found for the ctx timeout then return false - element is not visible
		slog.Debug("error in utils.IsSubElementVisible", "selector", sel, "error", err)

		return false
	}

	slog.Debug("current nodesList", "len", len(nodesList))

	// if 1 or more elements found then return true - element is visible
	return len(nodesList) > 0
}

// ParseDurationStringToSeconds parses a duration string in the format "HH:MM:SS" and returns the total number of seconds.
func ParseDurationStringToSeconds(durationStr string) (int, error) {
	var totalSeconds int

	slog.Debug("parse duration string to seconds", "string", durationStr)
	// define origin time for duration calculation as 00:00:00 UTC
	origin := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
	// define time layout for parsing duration string
	timeLayout := "15:04:05"

	parsedTime, err := time.Parse(timeLayout, durationStr)
	if err != nil {
		slog.Warn("error parsing duration string", "string", durationStr, "error", err)

		return -1, err
	}

	// calculate duration from origin to parsed time
	duration := parsedTime.Sub(origin)
	totalSeconds = int(duration.Seconds())

	slog.Debug("parsed duration string to seconds", "seconds", totalSeconds)

	return totalSeconds, nil
}

// SetPromGaugeNonNeg sets the Prometheus Gauge metric to the specified value if it is non-negative.
func SetPromGaugeNonNeg(promMetric prometheus.Gauge, value float64) {

	if value < 0 {
		slog.Error("value for Prometheus metric is negative", "metric", promMetric.Desc().String(), "value", value)

		return
	}

	promMetric.Set(value)
}
