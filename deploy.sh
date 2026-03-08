#!/bin/bash
# Complete deployment script for go-emailservice-ads

set -e

VERSION=${VERSION:-latest}
REGISTRY=${REGISTRY:-afterdarksys}
IMAGE_NAME="go-emailservice-ads"
FULL_IMAGE="${REGISTRY}/${IMAGE_NAME}:${VERSION}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║     Go Email Service - Complete Deployment Script           ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Parse command line arguments
COMMAND=${1:-help}

case "$COMMAND" in
  build)
    echo -e "${YELLOW}→ Building Docker image...${NC}"
    docker build --no-cache \
      -t ${FULL_IMAGE} \
      -t ${REGISTRY}/${IMAGE_NAME}:$(date +%Y%m%d) \
      .

    echo -e "${GREEN}✓ Image built: ${FULL_IMAGE}${NC}"
    docker images | grep ${IMAGE_NAME}
    ;;

  push)
    echo -e "${YELLOW}→ Pushing Docker image to registry...${NC}"

    # Check if logged in
    if ! docker info | grep -q "Username"; then
      echo -e "${RED}✗ Not logged in to Docker registry${NC}"
      echo "Run: docker login"
      exit 1
    fi

    docker push ${FULL_IMAGE}
    docker push ${REGISTRY}/${IMAGE_NAME}:$(date +%Y%m%d)

    echo -e "${GREEN}✓ Images pushed to registry${NC}"
    ;;

  test)
    echo -e "${YELLOW}→ Testing Docker image locally...${NC}"

    # Stop any existing test container
    docker stop mail-test 2>/dev/null || true
    docker rm mail-test 2>/dev/null || true

    # Run test container
    docker run -d \
      --name mail-test \
      -p 2525:2525 \
      -p 8080:8080 \
      -v $(pwd)/config.yaml:/opt/goemailservices/config.yaml:ro \
      ${FULL_IMAGE}

    echo "Waiting for service to start..."
    sleep 5

    # Health check
    if curl -f http://localhost:8080/health; then
      echo -e "${GREEN}✓ Service is healthy${NC}"

      # Test mailctl
      docker exec mail-test mailctl --api http://localhost:8080 --username admin --password changeme health

      echo -e "${GREEN}✓ Docker deployment test passed${NC}"
    else
      echo -e "${RED}✗ Service health check failed${NC}"
      docker logs mail-test
      exit 1
    fi
    ;;

  up)
    echo -e "${YELLOW}→ Starting services with Docker Compose...${NC}"

    COMPOSE_FILE=${2:-docker-compose.simple.yml}

    docker-compose -f ${COMPOSE_FILE} up -d

    echo "Waiting for services to start..."
    sleep 5

    # Check health
    if curl -f http://localhost:8080/health; then
      echo -e "${GREEN}✓ Services started successfully${NC}"
      docker-compose -f ${COMPOSE_FILE} ps
    else
      echo -e "${RED}✗ Service failed to start${NC}"
      docker-compose -f ${COMPOSE_FILE} logs
      exit 1
    fi
    ;;

  down)
    echo -e "${YELLOW}→ Stopping services...${NC}"

    COMPOSE_FILE=${2:-docker-compose.simple.yml}

    docker-compose -f ${COMPOSE_FILE} down

    echo -e "${GREEN}✓ Services stopped${NC}"
    ;;

  logs)
    COMPOSE_FILE=${2:-docker-compose.simple.yml}
    docker-compose -f ${COMPOSE_FILE} logs -f
    ;;

  clean)
    echo -e "${YELLOW}→ Cleaning up Docker resources...${NC}"

    # Stop all containers
    docker stop mail-test 2>/dev/null || true
    docker-compose -f docker-compose.yml down 2>/dev/null || true
    docker-compose -f docker-compose.simple.yml down 2>/dev/null || true

    # Remove containers
    docker rm mail-test 2>/dev/null || true

    # Remove images (optional)
    if [ "$2" = "--images" ]; then
      docker rmi ${FULL_IMAGE} 2>/dev/null || true
      docker rmi ${REGISTRY}/${IMAGE_NAME}:$(date +%Y%m%d) 2>/dev/null || true
      echo -e "${GREEN}✓ Images removed${NC}"
    fi

    echo -e "${GREEN}✓ Cleanup complete${NC}"
    ;;

  full-deploy)
    echo -e "${YELLOW}→ Full deployment (build + test + push)...${NC}"

    $0 build
    $0 test

    read -p "Push to registry? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
      $0 push
    fi

    echo -e "${GREEN}✓ Full deployment complete${NC}"
    ;;

  k8s-deploy)
    echo -e "${YELLOW}→ Deploying to Kubernetes...${NC}"

    if [ ! -f deploy/k8s/deployment.yaml ]; then
      echo -e "${RED}✗ Kubernetes manifests not found${NC}"
      exit 1
    fi

    kubectl apply -f deploy/k8s/

    echo "Waiting for deployment..."
    kubectl rollout status deployment/mail-primary

    echo -e "${GREEN}✓ Kubernetes deployment complete${NC}"
    kubectl get pods -l app=go-emailservice-ads
    ;;

  status)
    echo -e "${YELLOW}→ Service Status${NC}"
    echo ""

    # Docker containers
    echo "Docker Containers:"
    docker ps | grep -E "CONTAINER|emailservice|mail-" || echo "No containers running"
    echo ""

    # Docker images
    echo "Docker Images:"
    docker images | grep -E "REPOSITORY|emailservice" || echo "No images found"
    echo ""

    # Check if service is responding
    if curl -s http://localhost:8080/health > /dev/null; then
      echo -e "${GREEN}✓ Service is responding on port 8080${NC}"
      curl -s http://localhost:8080/health | jq .
    else
      echo -e "${YELLOW}⚠ Service not responding on port 8080${NC}"
    fi
    ;;

  help|*)
    cat <<EOF
Usage: $0 [COMMAND] [OPTIONS]

Commands:
  build           - Build Docker image
  push            - Push image to registry
  test            - Test Docker image locally
  up [compose]    - Start services with Docker Compose
  down [compose]  - Stop services
  logs [compose]  - View service logs
  clean [--images] - Clean up containers and optionally images
  full-deploy     - Build, test, and optionally push
  k8s-deploy      - Deploy to Kubernetes
  status          - Show deployment status
  help            - Show this help

Environment Variables:
  VERSION         - Image version tag (default: latest)
  REGISTRY        - Docker registry (default: afterdarksys)

Examples:
  # Build and test locally
  $0 build
  $0 test

  # Full deployment
  $0 full-deploy

  # Start with Docker Compose
  $0 up docker-compose.simple.yml

  # Deploy to Kubernetes
  $0 k8s-deploy

  # Check status
  $0 status

  # Clean up
  $0 clean --images
EOF
    ;;
esac
