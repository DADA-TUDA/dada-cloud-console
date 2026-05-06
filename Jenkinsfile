pipeline {
  agent any

  options {
    timestamps()
    disableConcurrentBuilds()
    buildDiscarder(logRotator(numToKeepStr: '20'))
  }

  parameters {
    string(name: 'IMAGE_TAG', defaultValue: '', description: 'Override image tag. Empty = short git SHA')
    booleanParam(name: 'PUSH_IMAGES', defaultValue: true, description: 'Push images to Nexus')
  }

  environment {
    NEXUS_REGISTRY = 'nexus.dada-tuda.ru'
    NEXUS_NAMESPACE = 'dada'
    BACKEND_IMAGE = "${NEXUS_REGISTRY}/${NEXUS_NAMESPACE}/dada-cloud-console-backend"
    FRONTEND_IMAGE = "${NEXUS_REGISTRY}/${NEXUS_NAMESPACE}/dada-cloud-console-frontend"
  }

  stages {
    stage('Checkout') {
      steps { checkout scm }
    }

    stage('Resolve version') {
      steps {
        script {
          env.GIT_SHA_SHORT = sh(script: 'git rev-parse --short=8 HEAD', returnStdout: true).trim()
          env.RESOLVED_TAG = params.IMAGE_TAG?.trim() ? params.IMAGE_TAG.trim() : env.GIT_SHA_SHORT
        }
        echo "Resolved image tag: ${env.RESOLVED_TAG}"
      }
    }

    stage('Backend test') {
      steps {
        dir('backend') {
          sh 'go test ./...'
        }
      }
    }

    stage('Frontend build check') {
      steps {
        dir('frontend') {
          sh 'npm ci'
          sh 'npm run build'
        }
      }
    }

    stage('Docker build') {
      steps {
        sh '''
          docker build -t ${BACKEND_IMAGE}:${RESOLVED_TAG} -f backend/Dockerfile backend
          docker build -t ${FRONTEND_IMAGE}:${RESOLVED_TAG} -f frontend/Dockerfile frontend
        '''
      }
    }

    stage('Docker push') {
      when { expression { return params.PUSH_IMAGES } }
      steps {
        withCredentials([usernamePassword(credentialsId: 'docker-nexus-admin-psws', usernameVariable: 'DOCKER_USERNAME', passwordVariable: 'DOCKER_LOGIN')]) {
          sh '''
            echo "${DOCKER_LOGIN}" | docker login ${NEXUS_REGISTRY} -u "${DOCKER_USERNAME}" --password-stdin
            docker push ${BACKEND_IMAGE}:${RESOLVED_TAG}
            docker push ${FRONTEND_IMAGE}:${RESOLVED_TAG}
          '''
        }
      }
    }

    stage('Render Helm') {
      steps {
        sh '''
          helm lint helm/dada-cloud-console
          helm template dada-cloud-console helm/dada-cloud-console \
            --namespace devops-tools \
            --set global.imageRegistry=${NEXUS_REGISTRY}/${NEXUS_NAMESPACE} \
            --set backend.image.tag=${RESOLVED_TAG} \
            --set frontend.image.tag=${RESOLVED_TAG} \
            > /tmp/dada-cloud-console-rendered.yaml
        '''
      }
    }
  }

  post {
    success {
      echo "Built DADA Cloud Console images with tag ${env.RESOLVED_TAG}"
      echo "Backend:  ${env.BACKEND_IMAGE}:${env.RESOLVED_TAG}"
      echo "Frontend: ${env.FRONTEND_IMAGE}:${env.RESOLVED_TAG}"
    }
  }
}
