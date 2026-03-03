import { listTodos, createTodo, deleteTodo, updateTodo, Todo } from "./api.gen";

const app = document.getElementById("app")!;

async function render() {
  app.innerHTML = "";

  // Form
  const form = document.createElement("form");
  form.innerHTML = `
    <input id="new-todo" type="text" placeholder="New todo..." required />
    <button type="submit">Add</button>
  `;
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const input = document.getElementById("new-todo") as HTMLInputElement;
    await createTodo({ text: input.value });
    render();
  });
  app.appendChild(form);

  // List
  const todos = await listTodos();
  const ul = document.createElement("ul");
  for (const todo of todos) {
    const li = document.createElement("li");
    li.style.textDecoration = todo.done ? "line-through" : "none";
    li.textContent = todo.text;

    const doneBtn = document.createElement("button");
    doneBtn.textContent = todo.done ? "Undo" : "Done";
    doneBtn.onclick = async () => {
      await updateTodo(todo.id, { text: todo.text, done: !todo.done });
      render();
    };

    const delBtn = document.createElement("button");
    delBtn.textContent = "Delete";
    delBtn.onclick = async () => {
      await deleteTodo(todo.id);
      render();
    };

    li.appendChild(doneBtn);
    li.appendChild(delBtn);
    ul.appendChild(li);
  }
  app.appendChild(ul);
}

render().catch(console.error);
