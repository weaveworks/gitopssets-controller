# GitOpsSets Controller Development Guide

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Project Overview

GitOpsSets Controller is a Kubernetes controller written in Go that provides declarative resource generation from multiple sources. It includes a main controller service and a CLI tool for offline generation. The project uses Kubebuilder framework and follows standard Kubernetes operator patterns.

## Working Effectively

### Prerequisites
- Go 1.24+ (required by go.mod)
- Docker (for container builds)
- kubectl (for Kubernetes interactions)
- make (for build automation)

### Bootstrap and Build
Run these commands in order to set up the development environment:

```bash
# Initial setup - generates CRDs, controllers, and validates code
make manifests generate fmt vet
# Takes 3-4 minutes on first run. NEVER CANCEL. Set timeout to 5+ minutes.

# Build the manager binary
make build
# Takes ~10 seconds. Produces bin/manager

# Build the CLI tool
go build -o bin/gitopssets-cli ./cmd/gitopssets-cli
# Takes ~5 seconds. Produces bin/gitopssets-cli
```

### Running Tests
```bash
# Run unit tests
make test
# Takes 30-45 seconds. NEVER CANCEL. Set timeout to 2+ minutes.

# Run end-to-end tests
make e2e-tests
# Takes 10-20 seconds. NEVER CANCEL. Set timeout to 1+ minutes.
```

### Code Quality and Documentation
```bash
# Format code (always run before committing)
make fmt

# Vet code for issues (always run before committing)
make vet

# Generate API documentation
make api-docs
# Takes 30-40 seconds. NEVER CANCEL. Set timeout to 2+ minutes.
```

## Running the Application

### Controller (for development with Kubernetes cluster)
```bash
# Install CRDs into your cluster (requires kubectl access)
make install

# Run the controller locally against your cluster
make run
# Runs controller in foreground. Requires Flux components installed:
# flux install --components source-controller,kustomize-controller

# Alternative: run with specific arguments
make run RUN_ARGS="--default-service-account=my-sa"
```

### CLI Tool (for offline generation and testing)
```bash
# Test CLI functionality with an example
./bin/gitopssets-cli generate --disable-cluster-access examples/list-generator/list-generator.yaml

# Help for all commands
./bin/gitopssets-cli --help
./bin/gitopssets-cli generate --help
```

## Validation and Testing

### Manual Validation Requirements
Always manually validate changes by running through these scenarios:

1. **Build Validation**: Run complete build process
   ```bash
   make manifests generate fmt vet build
   ```

2. **CLI Functionality Test**: Verify CLI can generate resources
   ```bash
   ./bin/gitopssets-cli generate --disable-cluster-access examples/list-generator/list-generator.yaml
   # Should produce 3 Kustomization resources (dev, production, staging)
   ```

3. **Example Validation**: Test different generator types that work without cluster access
   ```bash
   # Test List generator (always works)
   ./bin/gitopssets-cli generate --disable-cluster-access examples/list-generator/list-generator.yaml
   
   # Test Matrix generator with static lists
   ./bin/gitopssets-cli generate --disable-cluster-access examples/matrix-generator/matrix-single-element.yaml
   
   # Test Repeated List generator
   ./bin/gitopssets-cli generate --disable-cluster-access examples/repeated-list/repeated-list-generator.yaml
   ```

4. **API Documentation**: Ensure docs build successfully
   ```bash
   make api-docs
   # Check that docs/api/gitopsset.md is updated
   ```

### Pre-commit Validation
Always run these before committing changes:
```bash
make fmt vet test
# Ensures code is formatted, passes static analysis, and all tests pass
```

## Docker and Deployment

### Container Build
```bash
# Build Docker image
make docker-build IMG=my-registry/gitopssets-controller:tag
# Takes 2-5 minutes depending on network. NEVER CANCEL. Set timeout to 10+ minutes.

# Build and push
make docker-build docker-push IMG=my-registry/gitopssets-controller:tag
```

### Kubernetes Deployment
```bash
# Deploy to cluster
make deploy IMG=my-registry/gitopssets-controller:tag

# Generate release manifest
make release IMG=my-registry/gitopssets-controller:tag
# Creates release.yaml file for cluster deployment

# Clean up
make undeploy
make uninstall
```

## Common Development Tasks

### Code Generation
When modifying APIs in `api/v1alpha1/`, always regenerate:
```bash
make manifests generate
# Updates CRDs and generated code
```

### Adding New Generators
1. Create generator in `pkg/generators/`
2. Add to enabled generators list in `main.go`
3. Add example in `examples/` directory
4. Test with CLI tool
5. Add to documentation

### Testing Changes
1. Run unit tests: `make test`
2. Test CLI with examples: `./bin/gitopssets-cli generate --disable-cluster-access examples/*/`
3. Run e2e tests: `make e2e-tests`
4. Manual validation with real cluster if needed

## Repository Structure Reference

### Key Directories
```
├── api/v1alpha1/          # GitOpsSet API definitions
├── cmd/gitopssets-cli/    # CLI tool source
├── controllers/           # Controller logic
├── pkg/generators/        # Generator implementations
├── examples/              # Example GitOpsSet configurations
├── docs/                  # Documentation
├── config/                # Kubernetes manifests and kustomize configs
├── tests/e2e/            # End-to-end tests
└── hack/                  # Build scripts and tooling
```

### Example Types Available
- `list-generator/` - Static list of elements (✓ works offline)
- `repeated-list/` - Repeated list generation (✓ works offline)
- `matrix-generator/matrix-single-element.yaml` - Matrix with static lists (✓ works offline)
- `gitrepository/` - Generate from Git repository contents (requires cluster)
- `ocirepository/` - Generate from OCI repository (requires cluster)
- `matrix-generator/matrix-generator.yaml` - Matrix with GitRepository (requires cluster)
- `config/` - Generate from ConfigMap/Secret (requires cluster)
- `pull-requests/` - Generate from pull requests (requires cluster)
- `cluster-generator/` - Generate from cluster resources (requires cluster)
- `imagepolicy/` - Generate from image policies (requires cluster)

### Important Files
- `Makefile` - Complete build automation
- `main.go` - Controller entry point
- `go.mod` - Go dependencies (requires Go 1.24+)
- `.github/workflows/ci.yaml` - CI pipeline
- `README.md` - Project overview and basic usage
- `docs/README.md` - Detailed documentation on generators and templating
- `docs/api/gitopsset.md` - Auto-generated API reference
- `examples/pull-requests/README.md` - Specific guide for pull request generator

## Troubleshooting

### Build Issues
- Ensure Go 1.24+ is installed: `go version`
- Clean and rebuild: `make clean && make build` (if clean target exists)
- Check GOPATH and module cache: `go clean -modcache`

### Test Failures
- Run individual test packages: `go test ./pkg/generators/...`
- Check cluster connectivity for e2e tests
- Verify Kubebuilder assets: `make envtest`

### Controller Issues
- Check cluster access: `kubectl cluster-info`
- Verify CRDs installed: `kubectl get crd gitopssets.sets.gitops.pro`
- Check required Flux components: `flux check`

## Performance Notes

- **NEVER CANCEL**: Build and test commands may take several minutes
- Code generation (`make manifests generate`) is the longest step (~3-4 minutes)
- API documentation generation takes ~30-40 seconds
- Unit tests complete in under 1 minute
- E2e tests complete in under 30 seconds
- CLI operations are near-instant for examples

## Security Considerations

- Never commit credentials or sensitive data
- Use service accounts for cluster operations
- Review generated YAML before applying to clusters
- Disable cluster access for CLI testing when possible (`--disable-cluster-access`)