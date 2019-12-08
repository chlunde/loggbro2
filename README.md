# loggbro2

## Running on k3s

    kubectl run loggbro --image=chlunde/loggbro:latest --env="HUMIO_TOKEN=..." --expose --port=514
