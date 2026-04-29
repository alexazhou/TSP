#!/Volumes/PData/GitDB/GTAgentHands/.venv/bin/python3
# tools/tsp_tester.py
# TSP Server Visual Tester (tkinter GUI)
# Dependencies: websocket-client (pip install websocket-client)

import tkinter as tk
from tkinter import ttk, filedialog, scrolledtext, messagebox
import json
import subprocess
import threading
import uuid
import time

try:
    import websocket
    HAS_WS = True
except ImportError:
    HAS_WS = False


# ---------------------------------------------------------------------------
# Connection Layer
# ---------------------------------------------------------------------------

class ConnectionBase:
    def __init__(self):
        self._pending: dict = {}          # id -> {"event": threading.Event, "result": None}
        self._lock = threading.Lock()
        self._connected = False
        self._reader_thread_handle = None

    def connect(self):
        """Open transport, start reader thread, send initialize, return tools list."""
        raise NotImplementedError

    def disconnect(self):
        raise NotImplementedError

    def call(self, method: str, tool: str | None, input_dict: dict, timeout: float = 30.0) -> dict:
        """Send a TSP request and block until response (or timeout)."""
        req_id = str(uuid.uuid4())
        event = threading.Event()
        with self._lock:
            self._pending[req_id] = {"event": event, "result": None}

        msg: dict = {"id": req_id, "method": method, "input": input_dict}
        if tool is not None:
            msg["tool"] = tool
        self._send(json.dumps(msg))

        if not event.wait(timeout):
            with self._lock:
                self._pending.pop(req_id, None)
            raise TimeoutError(f"No response for id={req_id} within {timeout}s")

        with self._lock:
            entry = self._pending.pop(req_id)
        return entry["result"]

    def _send(self, text: str):
        raise NotImplementedError

    def _reader_loop(self):
        raise NotImplementedError

    def _dispatch(self, line: str):
        """Parse a JSON line and wake the matching pending call."""
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            return
        req_id = msg.get("id")
        if req_id is None:
            return
        with self._lock:
            entry = self._pending.get(req_id)
        if entry is None:
            return
        entry["result"] = msg
        entry["event"].set()


class StdioConnection(ConnectionBase):
    def __init__(self, executable: str):
        super().__init__()
        self._executable = executable
        self._proc: subprocess.Popen | None = None

    def connect(self):
        self._proc = subprocess.Popen(
            [self._executable],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        self._connected = True
        self._reader_thread_handle = threading.Thread(target=self._reader_loop, daemon=True)
        self._reader_thread_handle.start()
        resp = self.call("initialize", None, {})
        return resp.get("result", {})

    def disconnect(self):
        self._connected = False
        if self._proc:
            try:
                self._proc.stdin.close()
                self._proc.terminate()
                self._proc.wait(timeout=3)
            except Exception:
                pass
            self._proc = None

    def _send(self, text: str):
        if self._proc and self._proc.stdin:
            self._proc.stdin.write((text + "\n").encode())
            self._proc.stdin.flush()

    def _reader_loop(self):
        try:
            for raw in self._proc.stdout:
                line = raw.decode().strip()
                if line:
                    self._dispatch(line)
        except Exception:
            pass


class WebSocketConnection(ConnectionBase):
    def __init__(self, url: str):
        super().__init__()
        self._url = url
        self._ws = None

    def connect(self):
        if not HAS_WS:
            raise ImportError("websocket-client is not installed. Run: pip install websocket-client")
        self._ws = websocket.create_connection(self._url, timeout=10)
        self._connected = True
        self._reader_thread_handle = threading.Thread(target=self._reader_loop, daemon=True)
        self._reader_thread_handle.start()
        resp = self.call("initialize", None, {})
        return resp.get("result", {})

    def disconnect(self):
        self._connected = False
        if self._ws:
            try:
                self._ws.close()
            except Exception:
                pass
            self._ws = None

    def _send(self, text: str):
        if self._ws:
            self._ws.send(text)

    def _reader_loop(self):
        try:
            while self._connected:
                line = self._ws.recv()
                if line:
                    self._dispatch(line)
        except Exception:
            pass


# ---------------------------------------------------------------------------
# GUI Application
# ---------------------------------------------------------------------------

class TSPTesterApp(tk.Tk):
    def __init__(self):
        super().__init__()
        self.title("TSP Server Tester")
        self.geometry("900x680")
        self.minsize(700, 500)

        self._conn: ConnectionBase | None = None
        self._tools: list[dict] = []
        self._current_tool: dict | None = None
        self._input_widgets: list[dict] = []  # [{name, widget, var, type}]

        self._build_ui()

    # -----------------------------------------------------------------------
    # UI Construction
    # -----------------------------------------------------------------------

    def _build_ui(self):
        self._build_connection_frame()

        paned = ttk.PanedWindow(self, orient=tk.HORIZONTAL)
        paned.pack(fill=tk.BOTH, expand=True, padx=6, pady=(0, 6))

        left = ttk.Frame(paned, width=180)
        paned.add(left, weight=1)
        self._build_tools_frame(left)

        right = ttk.Frame(paned)
        paned.add(right, weight=4)
        self._build_invoke_frame(right)

    def _build_connection_frame(self):
        frame = ttk.Frame(self, padding=6)
        frame.pack(fill=tk.X, padx=6, pady=6)

        # Row 0: Transport dropdown | path label | path entry (stretch) | Browse
        self._transport_var = tk.StringVar(value="stdio")
        ttk.Label(frame, text="Transport:").grid(row=0, column=0, sticky=tk.W)
        transport_cb = ttk.Combobox(
            frame, textvariable=self._transport_var,
            values=["stdio", "websocket"], state="readonly", width=12,
        )
        transport_cb.grid(row=0, column=1, sticky=tk.W, padx=(4, 12))
        transport_cb.bind("<<ComboboxSelected>>", lambda e: self._on_transport_change())

        self._path_label_var = tk.StringVar(value="Executable:")
        ttk.Label(frame, textvariable=self._path_label_var).grid(row=0, column=2, sticky=tk.W)

        self._path_var = tk.StringVar()
        ttk.Entry(frame, textvariable=self._path_var).grid(
            row=0, column=3, sticky=tk.EW, padx=(4, 0))

        self._browse_btn = ttk.Button(frame, text="Browse", command=self._on_browse)
        self._browse_btn.grid(row=0, column=4, padx=(4, 0))

        frame.columnconfigure(3, weight=1)

        # Row 1: Status (left) | Connect + Disconnect (right)
        row1 = ttk.Frame(frame)
        row1.grid(row=1, column=0, columnspan=5, sticky=tk.EW, pady=(6, 0))

        self._status_var = tk.StringVar(value="Disconnected")
        ttk.Label(row1, text="Status:").pack(side=tk.LEFT)
        self._status_label = ttk.Label(row1, textvariable=self._status_var, foreground="gray")
        self._status_label.pack(side=tk.LEFT, padx=(4, 0))

        sep = ttk.Separator(row1, orient=tk.VERTICAL)
        sep.pack(side=tk.LEFT, fill=tk.Y, padx=10, pady=2)

        self._proto_var = tk.StringVar()
        self._server_name_var = tk.StringVar()
        self._server_version_var = tk.StringVar()

        self._workspace_var = tk.StringVar()

        for label, var in [
            ("Protocol:", self._proto_var),
            ("Server:", self._server_name_var),
            ("Version:", self._server_version_var),
            ("Workspace:", self._workspace_var),
        ]:
            ttk.Label(row1, text=label).pack(side=tk.LEFT)
            ttk.Label(row1, textvariable=var, foreground="gray").pack(side=tk.LEFT, padx=(2, 10))

        self._disconnect_btn = ttk.Button(row1, text="Disconnect", command=self._on_disconnect, state=tk.DISABLED)
        self._disconnect_btn.pack(side=tk.RIGHT)
        self._connect_btn = ttk.Button(row1, text="Connect", command=self._on_connect)
        self._connect_btn.pack(side=tk.RIGHT, padx=(0, 6))

    def _build_tools_frame(self, parent):
        ttk.Label(parent, text="Tools").pack(anchor=tk.W, padx=4)
        frame = ttk.Frame(parent)
        frame.pack(fill=tk.BOTH, expand=True)

        scrollbar = ttk.Scrollbar(frame, orient=tk.VERTICAL)
        self._tool_listbox = tk.Listbox(frame, yscrollcommand=scrollbar.set, selectmode=tk.SINGLE, width=20,
                                        highlightthickness=0, borderwidth=0)
        scrollbar.config(command=self._tool_listbox.yview)
        scrollbar.pack(side=tk.RIGHT, fill=tk.Y)
        self._tool_listbox.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)
        self._tool_listbox.bind("<<ListboxSelect>>", self._on_tool_select)

    def _build_invoke_frame(self, parent):
        # Tool name + description
        header = ttk.Frame(parent)
        header.pack(fill=tk.X, padx=6, pady=(6, 0))

        ttk.Label(header, text="Tool:").grid(row=0, column=0, sticky=tk.W)
        self._tool_name_var = tk.StringVar(value="(select a tool)")
        ttk.Label(header, textvariable=self._tool_name_var, font=("TkDefaultFont", 10, "bold")).grid(
            row=0, column=1, sticky=tk.W, padx=4)

        ttk.Label(header, text="Desc:").grid(row=1, column=0, sticky=tk.W)
        self._tool_desc_var = tk.StringVar()
        ttk.Label(header, textvariable=self._tool_desc_var, wraplength=500, justify=tk.LEFT).grid(
            row=1, column=1, sticky=tk.W, padx=4)

        ttk.Separator(parent, orient=tk.HORIZONTAL).pack(fill=tk.X, padx=6, pady=4)

        # Inputs scroll area
        ttk.Label(parent, text="Inputs", font=("TkDefaultFont", 9, "bold")).pack(anchor=tk.W, padx=6)

        inputs_outer = ttk.Frame(parent)
        inputs_outer.pack(fill=tk.X, padx=6)

        self._inputs_canvas = tk.Canvas(inputs_outer, height=220, highlightthickness=0)
        inputs_scroll = ttk.Scrollbar(inputs_outer, orient=tk.VERTICAL, command=self._inputs_canvas.yview)
        self._inputs_canvas.configure(yscrollcommand=inputs_scroll.set)
        inputs_scroll.pack(side=tk.RIGHT, fill=tk.Y)
        self._inputs_canvas.pack(side=tk.LEFT, fill=tk.BOTH, expand=True)

        self._inputs_frame = ttk.Frame(self._inputs_canvas)
        self._inputs_frame_id = self._inputs_canvas.create_window((0, 0), window=self._inputs_frame, anchor=tk.NW)
        self._inputs_frame.bind("<Configure>", self._on_inputs_frame_configure)
        self._inputs_canvas.bind("<Configure>", self._on_inputs_canvas_configure)

        # Invoke button
        invoke_row = ttk.Frame(parent)
        invoke_row.pack(fill=tk.X, padx=6, pady=4)
        self._invoke_btn = ttk.Button(invoke_row, text="▶ Invoke", command=self._on_invoke, state=tk.DISABLED)
        self._invoke_btn.pack(side=tk.LEFT)

        ttk.Separator(parent, orient=tk.HORIZONTAL).pack(fill=tk.X, padx=6, pady=4)

        # Output area
        ttk.Label(parent, text="Output", font=("TkDefaultFont", 9, "bold")).pack(anchor=tk.W, padx=6)
        self._output_text = scrolledtext.ScrolledText(parent, height=10, state=tk.DISABLED,
                                                       font=("Courier", 10))
        self._output_text.pack(fill=tk.BOTH, expand=True, padx=6, pady=(0, 6))
        self._output_text.tag_config("error", foreground="red")

    def _on_inputs_frame_configure(self, event):
        self._inputs_canvas.configure(scrollregion=self._inputs_canvas.bbox("all"))

    def _on_inputs_canvas_configure(self, event):
        self._inputs_canvas.itemconfig(self._inputs_frame_id, width=event.width)

    # -----------------------------------------------------------------------
    # Connection Logic
    # -----------------------------------------------------------------------

    def _on_transport_change(self):
        t = self._transport_var.get()
        if t == "stdio":
            self._path_label_var.set("Executable:")
            self._browse_btn.config(state=tk.NORMAL)
        else:
            self._path_label_var.set("WebSocket URL:")
            self._browse_btn.config(state=tk.DISABLED)
            if not self._path_var.get():
                self._path_var.set("ws://localhost:8080/tsp")

    def _on_browse(self):
        path = filedialog.askopenfilename(title="Select TSP Server Executable")
        if path:
            self._path_var.set(path)

    def _on_connect(self):
        path = self._path_var.get().strip()
        if not path:
            messagebox.showerror("Error", "Please specify a path or URL.")
            return

        self._set_status("Connecting…", "orange")
        self._connect_btn.config(state=tk.DISABLED)

        transport = self._transport_var.get()

        def do_connect():
            try:
                if transport == "stdio":
                    conn = StdioConnection(path)
                else:
                    conn = WebSocketConnection(path)
                result = conn.connect()
                self._conn = conn
                self.after(0, lambda r=result: self._on_connected(r))
            except Exception as e:
                self.after(0, lambda: self._on_connect_failed(str(e)))

        threading.Thread(target=do_connect, daemon=True).start()

    def _on_connected(self, result: dict):
        self._tools = result.get("capabilities", {}).get("tools", [])
        self._set_status("Connected", "green")
        self._connect_btn.config(state=tk.DISABLED)
        self._disconnect_btn.config(state=tk.NORMAL)

        # Show protocolVersion + serverInfo
        info = result.get("serverInfo", {})
        self._proto_var.set(result.get("protocolVersion", ""))
        self._server_name_var.set(info.get("name", ""))
        self._server_version_var.set(info.get("version", ""))
        self._workspace_var.set(result.get("workspace", ""))

        self._tool_listbox.delete(0, tk.END)
        for t in self._tools:
            name = t.get("name", "?")
            self._tool_listbox.insert(tk.END, name)

    def _on_connect_failed(self, msg: str):
        self._set_status("Disconnected", "gray")
        self._connect_btn.config(state=tk.NORMAL)
        messagebox.showerror("Connection Failed", msg)

    def _on_disconnect(self):
        if self._conn:
            try:
                self._conn.disconnect()
            except Exception:
                pass
            self._conn = None
        self._set_status("Disconnected", "gray")
        self._proto_var.set("")
        self._server_name_var.set("")
        self._server_version_var.set("")
        self._workspace_var.set("")
        self._connect_btn.config(state=tk.NORMAL)
        self._disconnect_btn.config(state=tk.DISABLED)
        self._invoke_btn.config(state=tk.DISABLED)
        self._tool_listbox.delete(0, tk.END)
        self._tool_name_var.set("(select a tool)")
        self._tool_desc_var.set("")
        self._clear_inputs()

    def _set_status(self, text: str, color: str):
        self._status_var.set(text)
        self._status_label.config(foreground=color)

    # -----------------------------------------------------------------------
    # Tool Selection
    # -----------------------------------------------------------------------

    def _on_tool_select(self, event=None):
        sel = self._tool_listbox.curselection()
        if not sel:
            return
        idx = sel[0]
        if idx >= len(self._tools):
            return
        tool = self._tools[idx]
        self._current_tool = tool
        self._tool_name_var.set(tool.get("name", "?"))
        self._tool_desc_var.set(tool.get("description", ""))
        schema = tool.get("input_schema", tool.get("inputSchema", {}))
        self._build_inputs_for_tool(schema)
        self._invoke_btn.config(state=tk.NORMAL)

    def _clear_inputs(self):
        for widget in self._inputs_frame.winfo_children():
            widget.destroy()
        self._input_widgets = []

    def _build_inputs_for_tool(self, schema: dict):
        self._clear_inputs()

        properties = schema.get("properties", {})
        required_fields = set(schema.get("required", []))

        if not properties:
            ttk.Label(self._inputs_frame, text="(no inputs)").grid(row=0, column=0, sticky=tk.W, padx=4, pady=2)
            return

        for row_idx, (name, prop) in enumerate(properties.items()):
            is_required = name in required_fields
            label_text = f"* {name}" if is_required else f"  {name}"
            ttk.Label(self._inputs_frame, text=label_text, anchor=tk.W).grid(
                row=row_idx, column=0, sticky=tk.W, padx=(4, 8), pady=2)

            field_type = prop.get("type", "string")
            widget_info = self._make_input_widget(name, field_type, prop, row_idx)
            self._input_widgets.append({
                "name": name,
                "required": is_required,
                "type": field_type,
                **widget_info,
            })

        self._inputs_canvas.update_idletasks()
        self._inputs_canvas.configure(scrollregion=self._inputs_canvas.bbox("all"))

    def _make_input_widget(self, name: str, field_type: str, prop: dict, row: int) -> dict:
        """Create and grid the appropriate widget. Return dict with widget/var info."""

        multiline_names = ("content", "command", "pattern", "text", "body", "code")
        is_multiline = any(kw in name.lower() for kw in multiline_names)

        if field_type == "boolean":
            var = tk.BooleanVar(value=False)
            w = ttk.Checkbutton(self._inputs_frame, variable=var)
            w.grid(row=row, column=1, sticky=tk.W, padx=4, pady=2)
            return {"widget": w, "var": var, "widget_type": "checkbutton"}

        elif field_type in ("object", "array"):
            w = scrolledtext.ScrolledText(self._inputs_frame, height=4, width=50,
                                          font=("Courier", 10))
            w.grid(row=row, column=1, sticky=tk.EW, padx=4, pady=2)
            self._inputs_frame.columnconfigure(1, weight=1)
            return {"widget": w, "var": None, "widget_type": "scrolledtext"}

        elif (field_type == "string" and is_multiline) or field_type not in ("integer", "number", "string"):
            w = scrolledtext.ScrolledText(self._inputs_frame, height=3, width=50,
                                          font=("Courier", 10))
            w.grid(row=row, column=1, sticky=tk.EW, padx=4, pady=2)
            self._inputs_frame.columnconfigure(1, weight=1)
            return {"widget": w, "var": None, "widget_type": "scrolledtext"}

        else:
            var = tk.StringVar()
            w = ttk.Entry(self._inputs_frame, textvariable=var, width=50)
            w.grid(row=row, column=1, sticky=tk.EW, padx=4, pady=2)
            self._inputs_frame.columnconfigure(1, weight=1)
            return {"widget": w, "var": var, "widget_type": "entry"}

    # -----------------------------------------------------------------------
    # Invoke
    # -----------------------------------------------------------------------

    def _on_invoke(self):
        if not self._conn or not self._current_tool:
            return

        tool_name = self._current_tool.get("name", "")
        input_dict = {}

        for wi in self._input_widgets:
            name = wi["name"]
            wtype = wi["widget_type"]
            field_type = wi.get("type", "string")

            if wtype == "checkbutton":
                input_dict[name] = wi["var"].get()

            elif wtype == "scrolledtext":
                raw = wi["widget"].get("1.0", tk.END).strip()
                if raw:
                    if field_type in ("object", "array"):
                        try:
                            input_dict[name] = json.loads(raw)
                        except json.JSONDecodeError as e:
                            messagebox.showerror("Invalid JSON", f"Field '{name}': {e}")
                            return
                    else:
                        input_dict[name] = raw

            else:  # entry
                raw = wi["var"].get().strip()
                if raw:
                    if field_type in ("integer", "number"):
                        try:
                            input_dict[name] = int(raw) if field_type == "integer" else float(raw)
                        except ValueError:
                            messagebox.showerror("Invalid Input", f"Field '{name}' must be a number.")
                            return
                    else:
                        input_dict[name] = raw

            # Required validation
            if wi["required"] and name not in input_dict:
                messagebox.showerror("Missing Input", f"Field '{name}' is required.")
                return

        # Disable invoke button during call
        self._invoke_btn.config(state=tk.DISABLED)
        self._set_output("Invoking…", is_error=False)

        conn = self._conn

        def do_call():
            try:
                result = conn.call("tool", tool_name, input_dict)
                self.after(0, lambda: self._show_result(result))
            except Exception as e:
                self.after(0, lambda: self._show_result({"error": str(e)}, is_error=True))

        threading.Thread(target=do_call, daemon=True).start()

    def _show_result(self, result: dict, is_error: bool = False):
        self._invoke_btn.config(state=tk.NORMAL)
        # Detect error in TSP response
        if "error" in result and result.get("result") is None:
            is_error = True
        pretty = json.dumps(result, indent=2, ensure_ascii=False)
        self._set_output(pretty, is_error=is_error)

    def _set_output(self, text: str, is_error: bool = False):
        self._output_text.config(state=tk.NORMAL)
        self._output_text.delete("1.0", tk.END)
        tag = "error" if is_error else ""
        self._output_text.insert(tk.END, text, tag)
        self._output_text.config(state=tk.DISABLED)


# ---------------------------------------------------------------------------
# Entry Point
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    app = TSPTesterApp()
    app.mainloop()
