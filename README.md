# Fincons coding challenge

L'objectif de ce challenge est d'apporter des corrections et améliorations à un petit programme permettant d'exécuter un "HealthCheck" à partir d'un fichier contenant une liste de services web.

````bash
go run main.go services.txt

# Opening services.txt
# Url: https://stackoverflow.com; Status: 200; Latency: 130ms
# Url: https://www.google.com; Status: 200; Latency: 149ms
# Url: https://go.dev; Status: 200; Latency: 149ms
# Url: https://www.docker.com; Status: 200; Latency: 149ms
# Url: https://kubernetes.io; Status: 200; Latency: 168ms
# Url: https://www.finconsgroup.com; Status: 200; Latency: 168ms
````

### HealthCheck
La fonction `HealthCheck` prend en paramètre une liste d'URLs et retourne un tableau de résultats (status code, latence, etc...).
Les requêtes exécutées auprès des différents services listés dans le fichier `services.txt` sont effectuées de manière asynchrone.

L'implémentation de la fonction `HealthCheck` comporte actuellement quelques erreurs et les résultats retournés ne sont pas conformes aux attentes.

### Objectifs
#### 1. Identifiez et corrigez les erreurs de la fonction `HealthCheck`
Pour chacune des erreurs identifiées, décrivez clairement pourquoi elle engendre un résultat erroné et proposez une solution. 
N'hésitez pas à utiliser des commentaires pour dérouler toutes les étapes de votre raisonnement.

#### 2. Proposez une meilleure solution
Si l'on devait exécuter ce programme à partir d'un fichier contenant des millions d'URLs, l'architecture du programme actuel ne semble pas réaliste.
Implémentez une solution simple, scalable et idiomatique et n'oubliez pas de bien documenter pourquoi votre solution est meilleure que l'architecture actuelle.

### Livrable
Sous la forme d'une PR avec le label `ready for review`