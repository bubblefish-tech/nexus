package build

type CompiledSource struct {
Name     string            `json:"name"`
Endpoint string            `json:"endpoint"`
Color    string            `json:"color"`
CanRead  bool              `json:"can_read"`
CanWrite bool              `json:"can_write"`
Auth     struct {
Type   string `json:"type"`
Header string `json:"header"`
EnvVar string `json:"env_var"`
} `json:"auth"`
Mapping      map[string]string `json:"mapping"`
Enabled      bool              `json:"enabled"`
ErrorMessage string            `json:"error_message"`
}

type CompiledDestination struct {
Name           string   `json:"name"`
Type           string   `json:"type"`
Filepath       string   `json:"filepath"`
Table          string   `json:"table"`
BaseURL        string   `json:"base_url"`
AuthEnvVar     string   `json:"auth_env_var"`
RequiredFields []string `json:"required_fields"`
}
