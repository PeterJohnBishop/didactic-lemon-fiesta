///////////// Docker Containerization /////////////

// create a Docker Container
docker build -t peterjbishop/fuzzy-succotash-balance:latest .

// push the image to Docker Hub
docker push peterjbishop/fuzzy-succotash-balance

// pull a local image
docker pull peterjbishop/fuzzy-succotash-balance

// run Container in detached mode
docker run -d --name=fuzzy-succotash-balance -p 8080:8080 peterjbishop/fuzzy-succotash-balance

// run Container 
docker run --name=fuzzy-succotash-balance -p 8080:8080 peterjbishop/fuzzy-succotash-balance

docker run -p 8080:8080 peterjbishop/fuzzy-succotash-balance


///////////// Docker Compose (env set inline) /////////////
docker system prune -a

docker-compose down
docker-compose build --no-cache // create a fresh build!

GIN_PORT=:8080 \
PSQL_HOST=postgres \
PSQL_PORT=5432 \
PSQL_USER=postgres \
PSQL_PASSWORD=postgres \
PSQL_DBNAME=postgres \
TOKEN_SECRET=pjb.den \
REFRESH_TOKEN_SECRET=peterjbishop.denver \
docker-compose up // test locally

docker build -t peterjbishop/fuzzy-succotash-balance:latest . 
docker push peterjbishop/fuzzy-succotash-balance:latest 
kubectl rollout restart deployment fuzzy-succotash-balance



minikube status
minikybe start

kubectl delete deploy fuzzy-succotash-balance
kubectl delete deploy postgres

kubectl delete pods -l app=fuzzy-succotash-balance

kubectl get secret app-secret -o yaml


kubectl delete secret app-secret

kubectl create secret generic app-secret \
  --from-literal=GIN_PORT=":8080" \
  --from-literal=TOKEN_SECRET="pjb.den" \
  --from-literal=REFRESH_TOKEN_SECRET="peterjbishop.denver" \
  --from-literal=PSQL_USER=postgres \
  --from-literal=PSQL_PASSWORD=postgres \
  --from-literal=PSQL_DBNAME=postgres

kubectl apply -f deployment_database.yaml
kubectl apply -f deployment_server.yaml
kubectl expose deployment fuzzy-succotash-balance --type=NodePort --port=8080
minikube service fuzzy-succotash-balance