// Auto-generated TypeScript API client. Do not edit manually.
// Regenerate with: atlas codegen openapi http://localhost:7001

export interface Todo {
  id: string;
  text: string;
  done: boolean;
  created_at: string;
}

export interface CreateTodoRequest {
  text: string;
}

export interface UpdateTodoRequest {
  text: string;
  done: boolean;
}

export async function listTodos(baseURL = ""): Promise<Todo[]> {
  const res = await fetch(`${baseURL}/api/todos`);
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);
  return res.json() as Promise<Todo[]>;
}

export async function createTodo(body: CreateTodoRequest, baseURL = ""): Promise<Todo> {
  const res = await fetch(`${baseURL}/api/todos`, {
    method: "POST",
    body: JSON.stringify(body),
    headers: { "Content-Type": "application/json" },
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);
  return res.json() as Promise<Todo>;
}

export async function getTodo(id: string, baseURL = ""): Promise<Todo> {
  const res = await fetch(`${baseURL}/api/todos/${id}`);
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);
  return res.json() as Promise<Todo>;
}

export async function updateTodo(id: string, body: UpdateTodoRequest, baseURL = ""): Promise<Todo> {
  const res = await fetch(`${baseURL}/api/todos/${id}`, {
    method: "PUT",
    body: JSON.stringify(body),
    headers: { "Content-Type": "application/json" },
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);
  return res.json() as Promise<Todo>;
}

export async function deleteTodo(id: string, baseURL = ""): Promise<void> {
  const res = await fetch(`${baseURL}/api/todos/${id}`, { method: "DELETE" });
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);
}
