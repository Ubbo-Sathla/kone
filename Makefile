
echo:
	git pull
	docker buildx bake --load  -f  docker-compose.yaml