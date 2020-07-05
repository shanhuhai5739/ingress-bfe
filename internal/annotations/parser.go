package annotations

import (
	"errors"
	"fmt"
	"strings"

	networking "k8s.io/api/networking/v1beta1"
)

var (
	// ErrMissingAnnotations the ingress rule does not contain annotations
	// This is an error only when annotations are being parsed
	ErrMissingAnnotations = errors.New("ingress rule without annotations")

	// ErrInvalidAnnotationName the ingress rule does contains an invalid
	// annotation name
	ErrInvalidAnnotationName = errors.New("invalid annotation name")

	// AnnotationsPrefix defines the common prefix used in the nginx ingress controller
	AnnotationsPrefix = "bfe.ingress.kubernetes.io"
)

type ingAnnotations map[string]string

func (a ingAnnotations) parseString(name string) (string, error) {
	val, ok := a[name]
	if ok {
		s := normalizeString(val)
		if len(s) == 0 {
			return "", fmt.Errorf("the annotation %v does not contain a valid value (%v)", name, val)
		}

		return s, nil
	}
	return "", ErrMissingAnnotations
}

func normalizeString(input string) string {
	trimmedContent := []string{}
	for _, line := range strings.Split(input, "\n") {
		trimmedContent = append(trimmedContent, strings.TrimSpace(line))
	}

	return strings.Join(trimmedContent, "\n")
}

// GetStringAnnotation extracts a string from an Ingress annotation
func GetStringAnnotation(name string, ing *networking.Ingress) (string, error) {
	v := GetAnnotationWithPrefix(name)
	err := checkAnnotation(v, ing)
	if err != nil {
		return "", err
	}

	return ingAnnotations(ing.GetAnnotations()).parseString(v)
}

// GetAnnotationWithPrefix returns the prefix of ingress annotations
func GetAnnotationWithPrefix(suffix string) string {
	return fmt.Sprintf("%v/%v", AnnotationsPrefix, suffix)
}

func checkAnnotation(name string, ing *networking.Ingress) error {
	if ing == nil || len(ing.GetAnnotations()) == 0 {
		return ErrMissingAnnotations
	}
	if name == "" {
		return ErrInvalidAnnotationName
	}

	return nil
}
