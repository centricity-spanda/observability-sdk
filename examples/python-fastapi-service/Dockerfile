FROM python:3.11-slim

WORKDIR /app

# Upgrade pip
RUN pip install --no-cache-dir --upgrade pip

# Copy and install the observability Python SDK
# Build context MUST be the repo root (../../ from this Dockerfile)
COPY python-sdk /python-sdk
RUN pip install --no-cache-dir -e /python-sdk

# Copy the example FastAPI service code
COPY examples/python-fastapi-service /app

# Install app dependencies (SDK is already installed above)
RUN pip install --no-cache-dir -r requirements-docker.txt

# Default port; can be overridden via PORT env
ENV PORT=8081
EXPOSE 8081

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8081"]

