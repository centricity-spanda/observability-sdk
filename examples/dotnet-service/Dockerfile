# Build stage — context must be repo root to access dotnet-sdk project
FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
WORKDIR /src

# Copy SDK project first (dependency)
COPY dotnet-sdk/Centricity.Observability.csproj dotnet-sdk/
RUN dotnet restore dotnet-sdk/Centricity.Observability.csproj

# Copy example service project
COPY examples/dotnet-service/dotnet-service.csproj examples/dotnet-service/
RUN dotnet restore examples/dotnet-service/dotnet-service.csproj

# Copy all source
COPY dotnet-sdk/ dotnet-sdk/
COPY examples/dotnet-service/ examples/dotnet-service/

# Publish
WORKDIR /src/examples/dotnet-service
RUN dotnet publish -c Release -o /app

# Runtime stage
FROM mcr.microsoft.com/dotnet/aspnet:8.0
WORKDIR /app
COPY --from=build /app .

ENV ASPNETCORE_URLS=http://+:8091
EXPOSE 8091

ENTRYPOINT ["dotnet", "dotnet-service.dll"]
