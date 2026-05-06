package api

import "testing"

func TestValidateKubeName(t *testing.T) {
	valid := []string{"codex-lb-db", "profi-db", "a", "abc123", "a-b-c"}
	for _, name := range valid {
		if err := validateKubeName(name); err != nil {
			t.Errorf("validateKubeName(%q) unexpected error: %v", name, err)
		}
	}

	invalid := []string{"", "UpperCase", "-leading", "trailing-", "has spaces", "has_underscore",
		"toolongname-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for _, name := range invalid {
		if err := validateKubeName(name); err == nil {
			t.Errorf("validateKubeName(%q) expected error, got nil", name)
		}
	}
}

func TestValidatePgName(t *testing.T) {
	valid := []string{"codexlb", "profi_db", "my_database_1", "a"}
	for _, name := range valid {
		if err := validatePgName(name); err != nil {
			t.Errorf("validatePgName(%q) unexpected error: %v", name, err)
		}
	}

	invalid := []string{"", "1startswithdigit", "has-hyphen", "has space", "UpperCase",
		"toolongname_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for _, name := range invalid {
		if err := validatePgName(name); err == nil {
			t.Errorf("validatePgName(%q) expected error, got nil", name)
		}
	}
}
