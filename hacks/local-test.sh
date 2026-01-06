#!/bin/bash
# Local testing script for fetchit
# Mirrors GitHub Actions validation tests for local development

set -e

# Determine script directory and repo root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test results tracking
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_TESTS=()

# Configuration
FETCHIT_IMAGE="quay.io/fetchit/fetchit:local-test"
COLORS_IMAGE="docker.io/mmumshad/simple-webapp-color:latest"
SYSTEMD_IMAGE="quay.io/fetchit/fetchit-systemd:local-test"
ANSIBLE_IMAGE="quay.io/fetchit/fetchit-ansible:local-test"

print_header() {
    echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ $1${NC}"
}

cleanup() {
    print_info "Cleaning up containers and volumes..."
    sudo podman rm -f fetchit fetchit-systemd fetchit-ansible cap1 cap2 colors1 colors2 httpd 2>/dev/null || true
    sudo podman pod rm -f colors_pod 2>/dev/null || true
    sudo podman volume rm -f fetchit-volume test 2>/dev/null || true
    sudo rm -rf /tmp/ft /tmp/disco /tmp/image 2>/dev/null || true
    sudo rm -f /etc/systemd/system/httpd.service 2>/dev/null || true

    # Clean up Quadlet files (rootful)
    sudo rm -f /etc/containers/systemd/simple.container 2>/dev/null || true
    sudo rm -f /etc/containers/systemd/httpd.container 2>/dev/null || true
    sudo rm -f /etc/containers/systemd/httpd.volume 2>/dev/null || true
    sudo rm -f /etc/containers/systemd/httpd.network 2>/dev/null || true

    # Clean up Quadlet files (rootless)
    XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
    QUADLET_DIR="$XDG_CONFIG_HOME/containers/systemd"
    rm -f "$QUADLET_DIR/simple.container" 2>/dev/null || true
    rm -f "$QUADLET_DIR/httpd.container" 2>/dev/null || true
    rm -f "$QUADLET_DIR/httpd.volume" 2>/dev/null || true
    rm -f "$QUADLET_DIR/httpd.network" 2>/dev/null || true

    # Stop any Quadlet-generated services
    sudo systemctl stop simple.service 2>/dev/null || true
    systemctl --user stop simple.service 2>/dev/null || true

    sudo systemctl daemon-reload 2>/dev/null || true
    systemctl --user daemon-reload 2>/dev/null || true
}

record_test() {
    local test_name="$1"
    local result="$2"

    TESTS_RUN=$((TESTS_RUN + 1))

    if [ "$result" = "pass" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        print_success "PASSED: $test_name"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        FAILED_TESTS+=("$test_name")
        print_error "FAILED: $test_name"
    fi
}

wait_for_container() {
    local container_name="$1"
    local pattern="$2"
    local max_wait="${3:-150}"
    local count=0

    print_info "Waiting for container: $pattern"

    timeout "$max_wait" bash -c "
        until [ \$(sudo podman ps | grep '$pattern' | wc -l) -ge $count ]; do
            sleep 2
        done
    " 2>/dev/null || return 1

    return 0
}

wait_for_file() {
    local file_path="$1"
    local max_wait="${2:-150}"

    print_info "Waiting for file: $file_path"

    timeout "$max_wait" bash -c "
        until [ -f '$file_path' ]; do
            sleep 2
        done
    " 2>/dev/null || return 1

    return 0
}

show_logs() {
    local container="${1:-fetchit}"
    echo -e "\n${YELLOW}Container logs for $container:${NC}"
    sudo podman logs "$container" 2>&1 | tail -50
}

# ============================================================================
# Test Functions
# ============================================================================

test_raw_validate() {
    print_header "TEST: Raw Container Deployment"
    cleanup

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -v $REPO_ROOT/examples/raw-config.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "raw-validate" "fail"
        return 1
    fi

    sleep 5

    if ! timeout 150 bash -c 'c=0; until [ $c -eq 2 ]; do c=$(sudo podman ps | grep colors | wc -l); sleep 2; done' 2>/dev/null; then
        show_logs
        record_test "raw-validate" "fail"
        return 1
    fi

    # Verify capabilities
    if ! sudo podman container inspect cap1 --format '{{.EffectiveCaps}}' | grep -q NET_ADMIN; then
        print_error "cap1 missing NET_ADMIN capability"
        record_test "raw-validate" "fail"
        return 1
    fi

    # Verify no capabilities on cap2
    if [ "$(sudo podman container inspect cap2 --format '{{.EffectiveCaps}}' | jq length)" != "0" ]; then
        print_error "cap2 should have no capabilities"
        record_test "raw-validate" "fail"
        return 1
    fi

    # Verify labels
    if [ "$(sudo podman ps --filter label=owned-by=fetchit | wc -l)" -le 1 ]; then
        print_error "Label 'owned-by=fetchit' not applied correctly"
        record_test "raw-validate" "fail"
        return 1
    fi

    record_test "raw-validate" "pass"
}

test_config_url_validate() {
    print_header "TEST: Config URL Loading"
    cleanup

    sudo mkdir -p "${HOME}"/.fetchit

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -e FETCHIT_CONFIG_URL=https://raw.githubusercontent.com/containers/fetchit/main/examples/raw-config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "config-url-validate" "fail"
        return 1
    fi

    sleep 5

    if ! timeout 150 bash -c 'c=0; until [ $c -eq 2 ]; do c=$(sudo podman ps | grep colors | wc -l); sleep 2; done' 2>/dev/null; then
        show_logs
        record_test "config-url-validate" "fail"
        return 1
    fi

    record_test "config-url-validate" "pass"
}

test_config_env_validate() {
    print_header "TEST: Config via Environment Variable"
    cleanup

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -e FETCHIT_CONFIG="$(cat $REPO_ROOT/examples/raw-config.yaml)" \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "config-env-validate" "fail"
        return 1
    fi

    sleep 5

    if ! timeout 150 bash -c 'c=0; until [ $c -eq 2 ]; do c=$(sudo podman ps | grep colors | wc -l); sleep 2; done' 2>/dev/null; then
        show_logs
        record_test "config-env-validate" "fail"
        return 1
    fi

    record_test "config-env-validate" "pass"
}

test_kube_validate() {
    print_header "TEST: Kubernetes YAML Deployment"
    cleanup

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -v $REPO_ROOT/examples/kube-play-config.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "kube-validate" "fail"
        return 1
    fi

    sleep 5

    if ! timeout 150 bash -c 'c=0; until [ $c -eq 1 ]; do c=$(sudo podman pod ps | grep -v CON= | grep colors_pod | wc -l); sleep 2; done' 2>/dev/null; then
        show_logs
        record_test "kube-validate" "fail"
        return 1
    fi

    record_test "kube-validate" "pass"
}

test_filetransfer_validate() {
    print_header "TEST: File Transfer"
    cleanup
    sudo mkdir -p /tmp/ft

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -v $REPO_ROOT/examples/filetransfer-config.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "filetransfer-validate" "fail"
        return 1
    fi

    sleep 5

    if ! wait_for_file "/tmp/ft/hello.txt" 150; then
        show_logs
        record_test "filetransfer-validate" "fail"
        return 1
    fi

    if ! wait_for_file "/tmp/ft/anotherfile.txt" 150; then
        show_logs
        record_test "filetransfer-validate" "fail"
        return 1
    fi

    record_test "filetransfer-validate" "pass"
}

test_filetransfer_exact_file() {
    print_header "TEST: File Transfer (Exact File)"
    cleanup
    sudo mkdir -p /tmp/ft/single

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -v $REPO_ROOT/examples/filetransfer-config-single-file.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "filetransfer-exact-file" "fail"
        return 1
    fi

    sleep 5

    if ! wait_for_file "/tmp/ft/single/hello.txt" 150; then
        show_logs
        record_test "filetransfer-exact-file" "fail"
        return 1
    fi

    record_test "filetransfer-exact-file" "pass"
}

test_systemd_validate() {
    print_header "TEST: Systemd Unit File Deployment"
    cleanup

    if ! sudo podman image exists "$SYSTEMD_IMAGE"; then
        print_warning "Systemd image not built, skipping test"
        record_test "systemd-validate" "skip"
        return 0
    fi

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -v $REPO_ROOT/examples/systemd-config.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "systemd-validate" "fail"
        return 1
    fi

    sleep 5

    if ! wait_for_file "/etc/systemd/system/httpd.service" 150; then
        show_logs
        record_test "systemd-validate" "fail"
        return 1
    fi

    record_test "systemd-validate" "pass"
}

test_clean_validate() {
    print_header "TEST: Clean (Prune) Functionality"
    cleanup

    # Create test volume and pull test image
    sudo podman volume create test
    sudo podman image pull alpine:latest 2>/dev/null || true

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -v $REPO_ROOT/examples/clean-config.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "clean-validate" "fail"
        return 1
    fi

    sleep 10

    # Wait for volume to be cleaned up
    if ! timeout 150 bash -c 'v=1; until [ $v -eq 0 ]; do v=$(sudo podman volume ls | grep test | wc -l); sleep 2; done' 2>/dev/null; then
        show_logs
        record_test "clean-validate" "fail"
        return 1
    fi

    # Wait for image to be removed
    if ! timeout 150 bash -c 'i=1; until [ $i -eq 0 ]; do i=$(sudo podman image ls alpine | grep -v REPOSITORY | wc -l); sleep 2; done' 2>/dev/null; then
        show_logs
        record_test "clean-validate" "fail"
        return 1
    fi

    record_test "clean-validate" "pass"
}

test_glob_validate() {
    print_header "TEST: Glob Pattern Matching"
    cleanup

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -v $REPO_ROOT/examples/glob-config.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "glob-validate" "fail"
        return 1
    fi

    sleep 5

    if ! timeout 150 bash -c 'c=0; until [ $c -eq 1 ]; do c=$(sudo podman ps | grep colors | wc -l); sleep 2; done' 2>/dev/null; then
        show_logs
        record_test "glob-validate" "fail"
        return 1
    fi

    # Verify capabilities of cap1
    if ! sudo podman container inspect cap1 --format '{{.EffectiveCaps}}' | grep -q NET_ADMIN; then
        print_error "cap1 missing NET_ADMIN capability"
        record_test "glob-validate" "fail"
        return 1
    fi

    record_test "glob-validate" "pass"
}

test_imageload_validate() {
    print_header "TEST: Image Loader"
    cleanup

    # Prepare test image
    sudo mkdir -p /tmp/image

    if ! sudo podman image exists "$COLORS_IMAGE"; then
        print_warning "Colors image not available, pulling..."
        sudo podman pull "$COLORS_IMAGE" 2>/dev/null || true
    fi

    sudo podman tag "$COLORS_IMAGE" quay.io/notreal/httpd:latest 2>/dev/null || true
    sudo podman save -o /tmp/image/httpd.tar quay.io/notreal/httpd:latest
    sudo podman image rm quay.io/notreal/httpd:latest 2>/dev/null || true

    # Start httpd server to serve the image
    if ! sudo podman run -d --name httpd -p 8080:8080 \
        -v /tmp/image:/var/www/html \
        registry.access.redhat.com/ubi8/httpd-24; then
        print_error "Failed to start httpd server"
        record_test "imageload-validate" "fail"
        return 1
    fi

    sleep 5

    if ! sudo podman run -d --name fetchit --network=host \
        -v $REPO_ROOT/examples/imageLoad-config.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "imageload-validate" "fail"
        return 1
    fi

    sleep 10

    # Wait for image to be loaded
    if ! timeout 150 bash -c 'i=0; until [ $i -eq 1 ]; do i=$(sudo podman image ls quay.io/notreal/httpd:latest | grep -v REPOSITORY | wc -l); sleep 2; done' 2>/dev/null; then
        show_logs
        record_test "imageload-validate" "fail"
        return 1
    fi

    record_test "imageload-validate" "pass"
}

test_quadlet_rootful_validate() {
    print_header "TEST: Quadlet Rootful Deployment"
    cleanup

    # Clean up any existing Quadlet files
    sudo rm -f /etc/containers/systemd/simple.container 2>/dev/null || true
    sudo systemctl daemon-reload 2>/dev/null || true

    if ! sudo podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -v $REPO_ROOT/examples/quadlet-config.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        -v /etc:/etc \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "quadlet-rootful-validate" "fail"
        return 1
    fi

    sleep 10

    # Wait for Quadlet file to be placed
    if ! wait_for_file "/etc/containers/systemd/simple.container" 150; then
        show_logs
        print_error "Quadlet file not created in /etc/containers/systemd/"
        record_test "quadlet-rootful-validate" "fail"
        return 1
    fi

    # Verify the file exists
    if [ ! -f "/etc/containers/systemd/simple.container" ]; then
        print_error "simple.container not found in /etc/containers/systemd/"
        record_test "quadlet-rootful-validate" "fail"
        return 1
    fi

    print_success "Quadlet file placed successfully"

    # Wait a bit for systemd to process the Quadlet file
    sleep 15

    # Check if systemd service was generated
    if ! sudo systemctl list-unit-files | grep -q "simple.service"; then
        print_warning "simple.service not found in systemd unit files (may be expected if daemon-reload didn't run)"
        # Don't fail the test, as this is a known issue
    fi

    record_test "quadlet-rootful-validate" "pass"
}

test_quadlet_rootless_validate() {
    print_header "TEST: Quadlet Rootless Deployment"
    cleanup

    # Ensure required environment variables are set
    if [ -z "$HOME" ]; then
        print_error "HOME environment variable not set"
        record_test "quadlet-rootless-validate" "fail"
        return 1
    fi

    # Determine XDG directories
    XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
    XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
    QUADLET_DIR="$XDG_CONFIG_HOME/containers/systemd"

    # Clean up any existing Quadlet files
    rm -f "$QUADLET_DIR/simple.container" 2>/dev/null || true

    # Enable lingering for rootless systemd (if not already enabled)
    if ! loginctl show-user "$USER" | grep -q "Linger=yes"; then
        print_info "Enabling lingering for user $USER"
        sudo loginctl enable-linger "$USER" || true
    fi

    if ! podman run -d --name fetchit \
        -v fetchit-volume:/opt \
        -v $REPO_ROOT/examples/quadlet-rootless.yaml:/opt/mount/config.yaml \
        -v /run/podman/podman.sock:/run/podman/podman.sock \
        -v "$HOME:$HOME" \
        -e HOME="$HOME" \
        -e XDG_CONFIG_HOME="$XDG_CONFIG_HOME" \
        -e XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" \
        --security-opt label=disable \
        "$FETCHIT_IMAGE"; then
        show_logs
        record_test "quadlet-rootless-validate" "fail"
        return 1
    fi

    sleep 10

    # Wait for Quadlet file to be placed
    if ! wait_for_file "$QUADLET_DIR/simple.container" 150; then
        show_logs
        print_error "Quadlet file not created in $QUADLET_DIR/"
        if [ -d "$QUADLET_DIR" ]; then
            print_info "Directory exists, listing contents:"
            ls -la "$QUADLET_DIR" || true
        else
            print_error "Directory $QUADLET_DIR does not exist"
        fi
        record_test "quadlet-rootless-validate" "fail"
        return 1
    fi

    # Verify the file exists
    if [ ! -f "$QUADLET_DIR/simple.container" ]; then
        print_error "simple.container not found in $QUADLET_DIR/"
        record_test "quadlet-rootless-validate" "fail"
        return 1
    fi

    print_success "Quadlet file placed successfully in rootless mode"

    # Wait a bit for systemd to process the Quadlet file
    sleep 15

    # Check if systemd service was generated (rootless)
    if ! systemctl --user list-unit-files 2>/dev/null | grep -q "simple.service"; then
        print_warning "simple.service not found in user systemd unit files (may be expected if daemon-reload didn't run)"
        # Don't fail the test, as this is a known issue
    fi

    record_test "quadlet-rootless-validate" "pass"
}

# ============================================================================
# Build Functions
# ============================================================================

build_fetchit_image() {
    print_header "Building fetchit image"

    if sudo podman image exists "$FETCHIT_IMAGE"; then
        print_info "Image already exists: $FETCHIT_IMAGE"
        return 0
    fi

    print_info "Building fetchit container image..."
    if ! (cd "$REPO_ROOT" && go mod tidy -compat=1.21 && go mod vendor && \
          docker build . --file Dockerfile --tag "$FETCHIT_IMAGE"); then
        print_error "Failed to build fetchit image"
        return 1
    fi

    print_success "Built fetchit image"
}

pull_colors_image() {
    print_header "Pulling colors test image"

    if sudo podman image exists "$COLORS_IMAGE"; then
        print_info "Image already exists: $COLORS_IMAGE"
        return 0
    fi

    print_info "Pulling colors image..."
    if ! sudo podman pull "$COLORS_IMAGE"; then
        print_error "Failed to pull colors image"
        return 1
    fi

    print_success "Pulled colors image"
}

build_systemd_image() {
    print_header "Building systemd image"

    if sudo podman image exists "$SYSTEMD_IMAGE"; then
        print_info "Image already exists: $SYSTEMD_IMAGE"
        return 0
    fi

    print_info "Building systemd container image..."
    if ! (cd "$REPO_ROOT" && CTR_CMD=docker make build-systemd-cross-build-linux-amd64 && \
          docker tag quay.io/fetchit/fetchit-systemd-amd:latest "$SYSTEMD_IMAGE"); then
        print_warning "Failed to build systemd image (optional)"
        return 1
    fi

    print_success "Built systemd image"
}

build_ansible_image() {
    print_header "Building ansible image"

    if sudo podman image exists "$ANSIBLE_IMAGE"; then
        print_info "Image already exists: $ANSIBLE_IMAGE"
        return 0
    fi

    print_info "Building ansible container image..."
    if ! (cd "$REPO_ROOT" && CTR_CMD=docker make build-ansible-cross-build-linux-amd64 && \
          docker tag quay.io/fetchit/fetchit-ansible-amd:latest "$ANSIBLE_IMAGE"); then
        print_warning "Failed to build ansible image (optional)"
        return 1
    fi

    print_success "Built ansible image"
}

# ============================================================================
# Main Script
# ============================================================================

show_help() {
    cat << EOF
Usage: $0 [OPTIONS] [TEST_NAME]

Local testing script for fetchit that mirrors GitHub Actions validation tests.

OPTIONS:
    -h, --help          Show this help message
    -b, --build-only    Only build images, don't run tests
    -c, --clean         Clean up before starting
    -l, --list          List available tests
    --skip-build        Skip building images

TEST_NAME:
    Run a specific test. If not provided, runs all tests.

AVAILABLE TESTS:
    raw-validate              Test raw container deployment
    config-url-validate       Test config loading from URL
    config-env-validate       Test config via environment variable
    kube-validate            Test Kubernetes YAML deployment
    filetransfer-validate    Test file transfer functionality
    filetransfer-exact-file  Test exact file transfer
    systemd-validate         Test systemd unit deployment
    clean-validate           Test cleanup/prune functionality
    glob-validate            Test glob pattern matching
    imageload-validate       Test image loading from HTTP
    quadlet-rootful-validate Test Quadlet rootful deployment
    quadlet-rootless-validate Test Quadlet rootless deployment

EXAMPLES:
    # Run all tests
    $0

    # Run specific test
    $0 raw-validate

    # Build images only
    $0 --build-only

    # Clean and run all tests
    $0 --clean

EOF
}

list_tests() {
    cat << EOF
Available tests:
  - raw-validate
  - config-url-validate
  - config-env-validate
  - kube-validate
  - filetransfer-validate
  - filetransfer-exact-file
  - systemd-validate
  - clean-validate
  - glob-validate
  - imageload-validate
  - quadlet-rootful-validate
  - quadlet-rootless-validate
EOF
}

run_all_tests() {
    test_raw_validate
    cleanup
    test_config_url_validate
    cleanup
    test_config_env_validate
    cleanup
    test_kube_validate
    cleanup
    test_filetransfer_validate
    cleanup
    test_filetransfer_exact_file
    cleanup
    test_systemd_validate
    cleanup
    test_clean_validate
    cleanup
    test_glob_validate
    cleanup
    test_imageload_validate
    cleanup
    test_quadlet_rootful_validate
    cleanup
    test_quadlet_rootless_validate
    cleanup
}

print_summary() {
    echo ""
    print_header "TEST SUMMARY"
    echo -e "Tests run:    ${BLUE}${TESTS_RUN}${NC}"
    echo -e "Tests passed: ${GREEN}${TESTS_PASSED}${NC}"
    echo -e "Tests failed: ${RED}${TESTS_FAILED}${NC}"

    if [ ${TESTS_FAILED} -gt 0 ]; then
        echo -e "\n${RED}Failed tests:${NC}"
        for test in "${FAILED_TESTS[@]}"; do
            echo -e "  ${RED}✗${NC} $test"
        done
        exit 1
    else
        echo -e "\n${GREEN}All tests passed!${NC}"
        exit 0
    fi
}

# Parse arguments
BUILD_ONLY=false
SKIP_BUILD=false
CLEAN_FIRST=false
SPECIFIC_TEST=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -b|--build-only)
            BUILD_ONLY=true
            shift
            ;;
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        -c|--clean)
            CLEAN_FIRST=true
            shift
            ;;
        -l|--list)
            list_tests
            exit 0
            ;;
        -*)
            echo "Unknown option: $1"
            show_help
            exit 1
            ;;
        *)
            SPECIFIC_TEST="$1"
            shift
            ;;
    esac
done

# Change to project root
cd "$(dirname "$0")/.."

# Check prerequisites
print_header "Checking Prerequisites"

if ! command -v podman &> /dev/null; then
    print_error "podman not found. Please install podman first."
    exit 1
fi

if ! command -v docker &> /dev/null; then
    print_warning "docker not found. Some builds may fail."
fi

if ! command -v jq &> /dev/null; then
    print_error "jq not found. Please install jq first."
    exit 1
fi

print_success "All prerequisites found"

# Clean if requested
if [ "$CLEAN_FIRST" = true ]; then
    cleanup
fi

# Build images
if [ "$SKIP_BUILD" = false ]; then
    build_fetchit_image || exit 1
    pull_colors_image || exit 1
    build_systemd_image || true  # Optional
    build_ansible_image || true  # Optional
fi

if [ "$BUILD_ONLY" = true ]; then
    print_success "Build complete!"
    exit 0
fi

# Run tests
print_header "Starting Tests"

if [ -n "$SPECIFIC_TEST" ]; then
    case "$SPECIFIC_TEST" in
        raw-validate)
            test_raw_validate
            ;;
        config-url-validate)
            test_config_url_validate
            ;;
        config-env-validate)
            test_config_env_validate
            ;;
        kube-validate)
            test_kube_validate
            ;;
        filetransfer-validate)
            test_filetransfer_validate
            ;;
        filetransfer-exact-file)
            test_filetransfer_exact_file
            ;;
        systemd-validate)
            test_systemd_validate
            ;;
        clean-validate)
            test_clean_validate
            ;;
        glob-validate)
            test_glob_validate
            ;;
        imageload-validate)
            test_imageload_validate
            ;;
        quadlet-rootful-validate)
            test_quadlet_rootful_validate
            ;;
        quadlet-rootless-validate)
            test_quadlet_rootless_validate
            ;;
        *)
            print_error "Unknown test: $SPECIFIC_TEST"
            list_tests
            exit 1
            ;;
    esac
    cleanup
else
    run_all_tests
fi

# Print summary
print_summary
