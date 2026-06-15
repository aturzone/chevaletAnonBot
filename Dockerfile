# Python 3.12 base image (matches your current environment)
FROM python:3.12-slim

# Set working directory
WORKDIR /app

# Install system dependencies for PostgreSQL
RUN apt-get update && apt-get install -y \
    build-essential \
    libpq-dev \
    && rm -rf /var/lib/apt/lists/*

# Copy and install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code
COPY . .

# Create logs directory
RUN mkdir -p /app/logs

# Show Python output in real-time (unbuffered)
ENV PYTHONUNBUFFERED=1

# Run the bot
CMD ["python", "main.py"]
