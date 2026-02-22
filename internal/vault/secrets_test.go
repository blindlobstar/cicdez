package vault

import (
	"errors"
	"testing"
)

func TestParseSecrets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Secrets
		wantErr error
	}{
		{
			name:  "flat secrets",
			input: "DB_PASSWORD: secret123\nAPI_KEY: mykey\n",
			want: Secrets{
				"DB_PASSWORD": "secret123",
				"API_KEY":     "mykey",
			},
		},
		{
			name:  "empty",
			input: "",
			want:  Secrets{},
		},
		{
			name:    "nested map",
			input:   "database:\n  password: secret123\n",
			wantErr: ErrNestedSecret,
		},
		{
			name:    "nested array",
			input:   "keys:\n  - key1\n  - key2\n",
			wantErr: ErrNestedSecret,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSecrets([]byte(tt.input))

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("expected %d secrets, got %d", len(tt.want), len(got))
			}

			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("expected %s=%q, got %q", k, v, got[k])
				}
			}
		})
	}
}
