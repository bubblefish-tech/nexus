"""
title: BubbleFish Nexus Memory Filter
author: BubbleFish Technologies
date: 2026-04-07
version: 1.4
license: MIT
description: Saves conversations to BubbleFish Nexus and injects relevant memories into the system prompt so Ollama responds with full memory awareness.
requirements: requests
"""

from typing import List, Optional
from pydantic import BaseModel
import requests


# Open WebUI fires internal system prompts for title generation, tag generation,
# and follow-up suggestions. These are noise — not real memories. We detect and
# skip them to prevent database bloating and context pollution in retrieval.
OWUI_NOISE_MARKERS = [
    "### Task:",
    "JSON format:",
    "### Guidelines:",
    "### Output:",
    "### Chat History:",
    "### Examples:",
]


def _is_owui_internal(content: str) -> bool:
    """Return True if content looks like an Open WebUI internal system prompt."""
    for marker in OWUI_NOISE_MARKERS:
        if marker in content:
            return True
    return False


class Pipeline:
    class Valves(BaseModel):
        pipelines: List[str] = ["*"]
        priority: int = 0
        bfn_data_key: str = ""
        nexus_url: str = "http://host.docker.internal:8080"
        source_name: str = "default"
        query_destination: str = "sqlite"
        subject: str = "conversations"
        collection: str = "open-webui"
        save_user_messages: bool = True
        save_assistant_messages: bool = True
        inject_memories: bool = True
        max_memories: int = 5
        memory_retrieval_profile: str = "balanced"
        memory_system_prefix: str = "Relevant memories from previous conversations:"

    def __init__(self):
        self.type = "filter"
        self.name = "BubbleFish Nexus Memory"
        self.valves = self.Valves()

    async def on_startup(self):
        print(f"on_startup:{__name__}")

    async def on_shutdown(self):
        print(f"on_shutdown:{__name__}")

    async def on_valves_updated(self):
        print(f"on_valves_updated:{__name__}")

    def _search_nexus(self, query: str) -> list:
        """Search Nexus for memories relevant to the user's message.
        NOTE: Query endpoint uses destination name (sqlite), not source name (default).
        """
        if not self.valves.bfn_data_key:
            return []
        try:
            resp = requests.get(
                f"{self.valves.nexus_url}/query/{self.valves.query_destination}",
                headers={
                    "Authorization": f"Bearer {self.valves.bfn_data_key}",
                    "Content-Type": "application/json",
                },
                params={
                    "q": query,
                    "limit": self.valves.max_memories,
                    "profile": self.valves.memory_retrieval_profile,
                },
                timeout=5,
            )
            if resp.status_code == 200:
                data = resp.json()
                records = data.get("records", [])
                print(f"BFN: retrieved {len(records)} memories for query: {query[:60]}")
                return records
            else:
                print(f"BFN: search failed status={resp.status_code} body={resp.text[:200]}")
                return []
        except Exception as e:
            print(f"BFN search error (non-fatal): {e}")
            return []

    def _build_memory_context(self, records: list) -> str:
        """Format retrieved memories into a context block for the system prompt."""
        if not records:
            return ""
        lines = [self.valves.memory_system_prefix]
        for i, record in enumerate(records, 1):
            content = record.get("content", "").strip()
            role = record.get("role", "")
            if content and not _is_owui_internal(content):
                prefix = f"[{i}]"
                if role:
                    prefix += f" ({role})"
                lines.append(f"{prefix} {content}")
        if len(lines) <= 1:
            return ""
        return "\n".join(lines)

    def _inject_memories_into_body(self, body: dict, memory_context: str) -> dict:
        """
        Inject memory context into the system prompt.
        If a system message exists, prepend to it.
        If not, insert a new system message at the start.
        """
        if not memory_context:
            return body

        messages = body.get("messages", [])

        for msg in messages:
            if msg.get("role") == "system":
                existing = msg.get("content", "")
                msg["content"] = f"{memory_context}\n\n{existing}" if existing else memory_context
                print(f"BFN: injected memories into existing system prompt")
                return body

        messages.insert(0, {
            "role": "system",
            "content": memory_context,
        })
        body["messages"] = messages
        print(f"BFN: injected memories as new system prompt")
        return body

    def _write_to_nexus(self, content: str, role: str, model: str = "ollama"):
        """Write a single message to Nexus via the inbound API.
        NOTE: Inbound endpoint uses source name (default), not destination name.
        """
        if not self.valves.bfn_data_key:
            print("BFN: No bfn_data_key set, skipping write")
            return

        if _is_owui_internal(content):
            print(f"BFN: skipping Open WebUI internal prompt (role={role})")
            return

        if not content or len(content.strip()) < 3:
            return

        try:
            payload = {
                "message": {
                    "content": content,
                    "role": role,
                },
                "model": model,
                "subject": self.valves.subject,
                "collection": self.valves.collection,
            }
            url = f"{self.valves.nexus_url}/inbound/{self.valves.source_name}"
            resp = requests.post(
                url,
                headers={
                    "Authorization": f"Bearer {self.valves.bfn_data_key}",
                    "Content-Type": "application/json",
                },
                json=payload,
                timeout=5,
            )
            if resp.status_code == 200:
                print(f"BFN: memory written role={role} chars={len(content)}")
            else:
                print(f"BFN: write failed status={resp.status_code} body={resp.text[:200]}")
        except Exception as e:
            print(f"BFN write error (non-fatal): {e}")

    async def inlet(self, body: dict, user: Optional[dict] = None) -> dict:
        """
        Runs BEFORE the message is sent to the LLM.
        1. Captures the user message and writes it to Nexus.
        2. Searches Nexus for relevant memories.
        3. Injects memories into the system prompt so Ollama has full context.
        """
        messages = body.get("messages", [])
        model = body.get("model", "ollama")

        user_content = ""
        for msg in reversed(messages):
            if msg.get("role") == "user":
                user_content = msg.get("content", "")
                break

        if _is_owui_internal(user_content):
            return body

        if self.valves.save_user_messages and user_content:
            self._write_to_nexus(user_content, "user", model)

        if self.valves.inject_memories and user_content:
            records = self._search_nexus(user_content)
            memory_context = self._build_memory_context(records)
            if memory_context:
                body = self._inject_memories_into_body(body, memory_context)

        return body

    async def outlet(self, body: dict, user: Optional[dict] = None) -> dict:
        """
        Runs AFTER the LLM responds.
        Captures the assistant response and writes it to Nexus.
        """
        if self.valves.save_assistant_messages:
            messages = body.get("messages", [])
            model = body.get("model", "ollama")
            for msg in reversed(messages):
                if msg.get("role") == "assistant":
                    content = msg.get("content", "")
                    if content and isinstance(content, str):
                        self._write_to_nexus(content, "assistant", model)
                    break
        return body
