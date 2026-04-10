"""
title: BubbleFish Ollama Pipe
author: BubbleFish Technologies
date: 2026-04-07
version: 1.0
license: MIT
description: Proxies Ollama models through Pipelines so the BubbleFish Nexus memory filter can intercept all conversations.
requirements: requests
"""

from typing import List, Optional, Generator, Iterator
from pydantic import BaseModel
import requests
import json


class Pipeline:
    class Valves(BaseModel):
        ollama_url: str = "http://host.docker.internal:11434"
        model_ids: List[str] = ["llama3.1:8b", "mistral:latest", "gemma2:latest"]

    def __init__(self):
        self.type = "manifold"
        self.name = "Ollama (Nexus)"
        self.valves = self.Valves()

    def pipelines(self) -> List[dict]:
        """Return available Ollama models as pipeline manifold entries."""
        try:
            resp = requests.get(
                f"{self.valves.ollama_url}/api/tags",
                timeout=5,
            )
            if resp.status_code == 200:
                models = resp.json().get("models", [])
                return [
                    {"id": m["name"], "name": f"Ollama: {m['name']}"}
                    for m in models
                ]
        except Exception as e:
            print(f"BFN Pipe: failed to fetch Ollama models: {e}")

        # Fallback to configured model list
        return [{"id": m, "name": f"Ollama: {m}"} for m in self.valves.model_ids]

    def pipe(
        self,
        user_message: str,
        model_id: str,
        messages: List[dict],
        body: dict,
    ) -> Iterator[str]:
        """Forward the request to Ollama and stream the response."""
        try:
            payload = {
                "model": model_id,
                "messages": messages,
                "stream": True,
            }

            resp = requests.post(
                f"{self.valves.ollama_url}/api/chat",
                json=payload,
                stream=True,
                timeout=120,
            )

            for line in resp.iter_lines():
                if line:
                    try:
                        data = json.loads(line)
                        content = data.get("message", {}).get("content", "")
                        if content:
                            yield content
                        if data.get("done"):
                            break
                    except json.JSONDecodeError:
                        continue

        except Exception as e:
            yield f"BFN Pipe error: {e}"
