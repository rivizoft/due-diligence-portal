package pdf

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var variable = regexp.MustCompile(`\{\{([^{}]*)\}\}`)

func RenderText(template string, identity Identity, documentName string, at time.Time) (string, error) {
	values := map[string]string{"user_email": identity.Email, "user_name": identity.Name, "document_name": documentName, "date": at.UTC().Format("2006-01-02")}
	for _, m := range variable.FindAllStringSubmatch(template, -1) {
		if _, ok := values[m[1]]; !ok {
			return "", fmt.Errorf("unsupported watermark variable: %s", m[1])
		}
	}
	remainder := variable.ReplaceAllString(template, "")
	if strings.ContainsAny(remainder, "{}") {
		return "", fmt.Errorf("invalid watermark template")
	}
	return variable.ReplaceAllStringFunc(template, func(s string) string { return values[s[2:len(s)-2]] }), nil
}
