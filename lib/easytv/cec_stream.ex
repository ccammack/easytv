defmodule EasyTV.CECStream do
  use GenServer
  require Logger

  @max_line 65_536

  def start_link(url), do: GenServer.start_link(__MODULE__, url)

  @impl true
  def init(url) do
    {:ok, %{url: url, task: nil}, {:continue, :connect}}
  end

  @impl true
  def handle_continue(:connect, state) do
    task = Task.Supervisor.async_nolink(App.TaskSupervisor, fn -> connect(state.url) end)
    {:noreply, %{state | task: task}}
  end

  @impl true
  def handle_info({ref, result}, %{task: %Task{ref: ref}} = state) do
    Process.demonitor(ref, [:flush])
    reconnect(result, state)
  end

  def handle_info({:DOWN, ref, :process, _pid, reason}, %{task: %Task{ref: ref}} = state),
    do: reconnect(reason, state)

  def handle_info(:connect, state), do: {:noreply, state, {:continue, :connect}}

  defp reconnect(reason, state) do
    Logger.warning("CEC observation gap: #{inspect(reason)}; reconnecting in 2s")
    EasyTV.ConfigDispatcher.cec_event(%{type: :observation_gap, reason: reason})
    Process.send_after(self(), :connect, 2000)
    {:noreply, %{state | task: nil}}
  end

  @impl true
  def terminate(_reason, %{task: %Task{} = task}), do: Task.shutdown(task, :brutal_kill)
  def terminate(_reason, _state), do: :ok

  def decode_lines(buffer, chunk) do
    lines = :binary.split(buffer <> chunk, "\n", [:global])
    {complete, [rest]} = Enum.split(lines, -1)

    if byte_size(rest) > @max_line or Enum.any?(complete, &(byte_size(&1) > @max_line)) do
      {:error, :line_too_long}
    else
      Enum.reduce_while(complete, {:ok, [], rest}, fn line, {:ok, events, rest} ->
        case Jason.decode(line) do
          {:ok, %{"event" => name} = event} when is_binary(name) ->
            {:cont, {:ok, [event | events], rest}}

          invalid ->
            {:halt, {:error, {:invalid_event, invalid}}}
        end
      end)
      |> case do
        {:ok, events, rest} -> {:ok, Enum.reverse(events), rest}
        error -> error
      end
    end
  end

  defp connect(url) do
    request = {String.to_charlist(url <> "/events.ndjson"), []}

    case :httpc.request(:get, request, EasyTV.Cecmond.http_options(url, :infinity),
           sync: false,
           stream: {:self, :once},
           body_format: :binary
         ) do
      {:ok, id} ->
        try do
          receive_stream(id, nil, "")
        after
          :httpc.cancel_request(id)
        end

      error ->
        error
    end
  end

  defp receive_stream(id, stream, buffer) do
    receive do
      {:http, {^id, :stream_start, headers, pid}} ->
        content_type = :proplists.get_value(~c"content-type", headers, ~c"") |> to_string()

        if String.starts_with?(content_type, "application/x-ndjson") do
          Logger.info("CEC NDJSON stream connected")
          :httpc.stream_next(pid)
          receive_stream(id, pid, buffer)
        else
          {:error, {:content_type, content_type}}
        end

      {:http, {^id, :stream, chunk}} ->
        case decode_lines(buffer, chunk) do
          {:ok, events, rest} ->
            Enum.each(events, fn
              %{"event" => "stream_keepalive"} -> :ok
              event -> GenServer.call(EasyTV.ConfigDispatcher, {:cec_event, event}, :infinity)
            end)

            :httpc.stream_next(stream)
            receive_stream(id, stream, rest)

          error ->
            error
        end

      {:http, {^id, :stream_end, _}} ->
        {:error, if(buffer == "", do: :stream_ended, else: :truncated_line)}

      {:http, {^id, result}} ->
        {:error, result}
    after
      45_000 -> {:error, :idle_timeout}
    end
  end
end
