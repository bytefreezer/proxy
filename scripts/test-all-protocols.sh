#!/bin/bash
#
# Comprehensive Protocol Test Runner for ByteFreezer Proxy
#
# This script runs tests for all supported protocols (syslog, NetFlow v5/v9/IPFIX, sFlow)
# and provides comprehensive testing scenarios for the ByteFreezer Proxy.
#
# Usage:
#     ./test-all-protocols.sh [options]
#
# Requirements:
#     - Python 3.6+
#     - ByteFreezer Proxy running with appropriate port configurations
#

set -e

# Default configuration
PROXY_HOST="localhost"
BASE_PORT=2056
PYTHON_CMD="python3"
VERBOSE=false
QUICK_TEST=false
STRESS_TEST=false
VALIDATE_ONLY=false

# Protocol configurations (port mappings)
declare -A PROTOCOLS=(
    ["syslog"]="2056"
    ["netflow-v5"]="2057"
    ["netflow-v9"]="2058"
    ["ipfix"]="2059"
    ["sflow"]="2060"
)

# Test parameters
QUICK_COUNT=5
NORMAL_COUNT=20
STRESS_DURATION=30
STRESS_RATE=50

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_test() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

# Help function
show_help() {
    cat << EOF
ByteFreezer Proxy - Protocol Test Runner

USAGE:
    $0 [OPTIONS]

OPTIONS:
    -h, --help          Show this help message
    -H, --host HOST     Proxy host (default: localhost)
    -p, --port PORT     Base port number (default: 2056)
    -q, --quick         Run quick tests (5 packets per protocol)
    -s, --stress        Run stress tests
    -v, --verbose       Enable verbose output
    -V, --validate      Validate packet structures only
    --syslog-port PORT  Syslog port (default: 2056)
    --netflow-port PORT NetFlow port (default: 2057)
    --sflow-port PORT   sFlow port (default: 2060)

EXAMPLES:
    # Quick test all protocols
    $0 --quick

    # Stress test with verbose output
    $0 --stress --verbose

    # Test specific host and ports
    $0 --host 192.168.1.100 --syslog-port 514

    # Validate packet structures
    $0 --validate

PROTOCOL PORT MAPPING:
    Syslog:     ${PROTOCOLS[syslog]}
    NetFlow v5: ${PROTOCOLS[netflow-v5]}
    NetFlow v9: ${PROTOCOLS[netflow-v9]}
    IPFIX:      ${PROTOCOLS[ipfix]}
    sFlow:      ${PROTOCOLS[sflow]}

REQUIREMENTS:
    - Python 3.6+
    - ByteFreezer Proxy running with configured ports
    - Network connectivity to proxy host

EOF
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                show_help
                exit 0
                ;;
            -H|--host)
                PROXY_HOST="$2"
                shift 2
                ;;
            -p|--port)
                BASE_PORT="$2"
                shift 2
                ;;
            -q|--quick)
                QUICK_TEST=true
                shift
                ;;
            -s|--stress)
                STRESS_TEST=true
                shift
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -V|--validate)
                VALIDATE_ONLY=true
                shift
                ;;
            --syslog-port)
                PROTOCOLS[syslog]="$2"
                shift 2
                ;;
            --netflow-port)
                PROTOCOLS[netflow-v5]="$2"
                PROTOCOLS[netflow-v9]="$2"
                shift 2
                ;;
            --sflow-port)
                PROTOCOLS[sflow]="$2"
                shift 2
                ;;
            *)
                log_error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check Python
    if ! command -v $PYTHON_CMD &> /dev/null; then
        log_error "Python 3 not found. Please install Python 3.6+"
        exit 1
    fi
    
    local python_version=$($PYTHON_CMD --version 2>&1 | cut -d' ' -f2)
    log_info "Python version: $python_version"
    
    # Check script directory
    local script_dir=$(dirname "$0")
    for script in test-syslog.py test-netflow-v5.py test-netflow-v9.py test-sflow.py; do
        if [[ ! -f "$script_dir/$script" ]]; then
            log_error "Test script not found: $script"
            exit 1
        fi
    done
    
    log_success "All prerequisites met"
}

# Test proxy connectivity
test_connectivity() {
    log_info "Testing connectivity to $PROXY_HOST..."
    
    for protocol in "${!PROTOCOLS[@]}"; do
        local port=${PROTOCOLS[$protocol]}
        if timeout 3 bash -c "echo >/dev/udp/$PROXY_HOST/$port" 2>/dev/null; then
            log_success "$protocol port $port: reachable"
        else
            log_warning "$protocol port $port: not reachable (proxy may not be running)"
        fi
    done
}

# Validate packet structures
validate_packets() {
    log_info "Validating packet structures..."
    
    local script_dir=$(dirname "$0")
    local failed=0
    
    # Validate syslog
    log_test "Validating syslog messages..."
    if $PYTHON_CMD "$script_dir/test-syslog.py" --validate; then
        log_success "Syslog validation passed"
    else
        log_error "Syslog validation failed"
        ((failed++))
    fi
    
    # Validate NetFlow v5
    log_test "Validating NetFlow v5 packets..."
    if $PYTHON_CMD "$script_dir/test-netflow-v5.py" --validate; then
        log_success "NetFlow v5 validation passed"
    else
        log_error "NetFlow v5 validation failed"
        ((failed++))
    fi
    
    # Validate sFlow
    log_test "Validating sFlow packets..."
    if $PYTHON_CMD "$script_dir/test-sflow.py" --validate; then
        log_success "sFlow validation passed"
    else
        log_error "sFlow validation failed"
        ((failed++))
    fi
    
    if [[ $failed -eq 0 ]]; then
        log_success "All packet validations passed"
        return 0
    else
        log_error "$failed packet validation(s) failed"
        return 1
    fi
}

# Run protocol test
run_protocol_test() {
    local protocol=$1
    local script=$2
    local port=$3
    local extra_args=$4
    
    log_test "Testing $protocol on port $port..."
    
    local verbose_flag=""
    if [[ $VERBOSE == true ]]; then
        verbose_flag="--verbose"
    fi
    
    local count=$NORMAL_COUNT
    if [[ $QUICK_TEST == true ]]; then
        count=$QUICK_COUNT
    fi
    
    local script_dir=$(dirname "$0")
    local cmd="$PYTHON_CMD $script_dir/$script --host $PROXY_HOST --port $port --count $count $verbose_flag $extra_args"
    
    if [[ $VERBOSE == true ]]; then
        log_info "Running: $cmd"
    fi
    
    if eval $cmd; then
        log_success "$protocol test completed successfully"
        return 0
    else
        log_error "$protocol test failed"
        return 1
    fi
}

# Run stress test
run_stress_test() {
    local protocol=$1
    local script=$2
    local port=$3
    local extra_args=$4
    
    log_test "Stress testing $protocol on port $port..."
    
    local verbose_flag=""
    if [[ $VERBOSE == true ]]; then
        verbose_flag="--verbose"
    fi
    
    local script_dir=$(dirname "$0")
    local cmd="$PYTHON_CMD $script_dir/$script --host $PROXY_HOST --port $port --stress --stress-duration $STRESS_DURATION --stress-rate $STRESS_RATE $verbose_flag $extra_args"
    
    if [[ $VERBOSE == true ]]; then
        log_info "Running: $cmd"
    fi
    
    if eval $cmd; then
        log_success "$protocol stress test completed successfully"
        return 0
    else
        log_error "$protocol stress test failed"
        return 1
    fi
}

# Run syslog tests
test_syslog() {
    local port=${PROTOCOLS[syslog]}
    
    if [[ $STRESS_TEST == true ]]; then
        run_stress_test "Syslog" "test-syslog.py" "$port" ""
    else
        # Test RFC3164
        run_protocol_test "Syslog RFC3164" "test-syslog.py" "$port" "--type rfc3164"
        
        # Test RFC5424  
        run_protocol_test "Syslog RFC5424" "test-syslog.py" "$port" "--type rfc5424"
        
        # Test mixed
        run_protocol_test "Syslog Mixed" "test-syslog.py" "$port" "--type mixed"
    fi
}

# Run NetFlow tests
test_netflow() {
    local port_v5=${PROTOCOLS[netflow-v5]}
    local port_v9=${PROTOCOLS[netflow-v9]}
    local port_ipfix=${PROTOCOLS[ipfix]}
    
    if [[ $STRESS_TEST == true ]]; then
        run_stress_test "NetFlow v5" "test-netflow-v5.py" "$port_v5" ""
        run_stress_test "NetFlow v9" "test-netflow-v9.py" "$port_v9" "--protocol v9"
        run_stress_test "IPFIX" "test-netflow-v9.py" "$port_ipfix" "--protocol ipfix"
    else
        # Test NetFlow v5
        run_protocol_test "NetFlow v5" "test-netflow-v5.py" "$port_v5" ""
        
        # Test NetFlow v9
        run_protocol_test "NetFlow v9" "test-netflow-v9.py" "$port_v9" "--protocol v9"
        
        # Test IPFIX
        run_protocol_test "IPFIX" "test-netflow-v9.py" "$port_ipfix" "--protocol ipfix"
    fi
}

# Run sFlow tests
test_sflow() {
    local port=${PROTOCOLS[sflow]}
    
    if [[ $STRESS_TEST == true ]]; then
        run_stress_test "sFlow" "test-sflow.py" "$port" ""
    else
        # Test flow samples
        run_protocol_test "sFlow Flow" "test-sflow.py" "$port" "--type flow"
        
        # Test counter samples
        run_protocol_test "sFlow Counter" "test-sflow.py" "$port" "--type counter"
        
        # Test mixed samples
        run_protocol_test "sFlow Mixed" "test-sflow.py" "$port" "--type mixed"
    fi
}

# Run comprehensive test scenarios
run_test_scenarios() {
    if [[ $STRESS_TEST == true ]]; then
        log_info "Running stress test scenarios..."
        run_stress_test "sFlow Scenarios" "test-sflow.py" "${PROTOCOLS[sflow]}" "--scenarios"
    else
        log_info "Running comprehensive test scenarios..."
        local script_dir=$(dirname "$0")
        local verbose_flag=""
        if [[ $VERBOSE == true ]]; then
            verbose_flag="--verbose"
        fi
        
        $PYTHON_CMD "$script_dir/test-sflow.py" --host "$PROXY_HOST" --port "${PROTOCOLS[sflow]}" --scenarios $verbose_flag
    fi
}

# Main test execution
run_tests() {
    local failed=0
    
    log_info "Starting ByteFreezer Proxy protocol tests..."
    log_info "Host: $PROXY_HOST"
    log_info "Test mode: $(if [[ $QUICK_TEST == true ]]; then echo "Quick"; elif [[ $STRESS_TEST == true ]]; then echo "Stress"; else echo "Normal"; fi)"
    
    echo
    echo "Protocol Port Mapping:"
    for protocol in "${!PROTOCOLS[@]}"; do
        echo "  $protocol: ${PROTOCOLS[$protocol]}"
    done
    echo
    
    # Run tests
    if ! test_syslog; then ((failed++)); fi
    echo
    
    if ! test_netflow; then ((failed++)); fi
    echo
    
    if ! test_sflow; then ((failed++)); fi
    echo
    
    if ! run_test_scenarios; then ((failed++)); fi
    
    # Summary
    echo
    echo "=" * 60
    if [[ $failed -eq 0 ]]; then
        log_success "All protocol tests completed successfully!"
    else
        log_error "$failed test(s) failed"
        exit 1
    fi
}

# Generate test report
generate_report() {
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    local report_file="protocol-test-report-$(date '+%Y%m%d-%H%M%S').txt"
    
    cat > "$report_file" << EOF
ByteFreezer Proxy Protocol Test Report
Generated: $timestamp

Configuration:
- Host: $PROXY_HOST
- Test Mode: $(if [[ $QUICK_TEST == true ]]; then echo "Quick"; elif [[ $STRESS_TEST == true ]]; then echo "Stress"; else echo "Normal"; fi)
- Verbose: $VERBOSE

Protocol Ports:
$(for protocol in "${!PROTOCOLS[@]}"; do echo "- $protocol: ${PROTOCOLS[$protocol]}"; done)

Test Results:
- All tests completed successfully
- No errors detected

Note: This report shows successful completion. Check console output for detailed results.
EOF
    
    log_info "Test report generated: $report_file"
}

# Main function
main() {
    parse_args "$@"
    
    echo "ByteFreezer Proxy - Protocol Test Runner"
    echo "=" * 50
    
    check_prerequisites
    
    if [[ $VALIDATE_ONLY == true ]]; then
        validate_packets
        exit $?
    fi
    
    test_connectivity
    echo
    
    run_tests
    
    generate_report
    
    log_success "Protocol testing completed!"
}

# Run main function with all arguments
main "$@"