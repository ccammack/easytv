defmodule EasyTV.Cecmond do
  use GenServer
  require Logger

  def start_link(url), do: GenServer.start_link(__MODULE__, url, name: __MODULE__)

  def validate_url!(url) do
    base = String.trim_trailing(url, "/")
    uri = URI.parse(base)

    unless uri.scheme in ["http", "https"] and is_binary(uri.host) and uri.host != "" and
             uri.userinfo == nil and uri.query == nil and uri.fragment == nil and
             uri.path in [nil, ""] do
      raise ArgumentError, "--cec-url must be an HTTP(S) base URL without credentials or path"
    end

    base
  end

  def send_command(command) when is_map(command) do
    case Jason.encode(command) do
      {:ok, payload} ->
        dispatch(payload)

      {:error, reason} ->
        Logger.error("CEC command encoding failed: #{inspect(reason)}")
        {:error, :invalid_command}
    end
  end

  def send_command(command) do
    Logger.error("CEC command must be a JSON object: #{inspect(command)}")
    {:error, :invalid_command}
  end

  defp dispatch(payload) do
    case Process.whereis(__MODULE__) do
      nil ->
        Logger.error("CEC command failed: --cec-url is not enabled")
        {:error, :not_running}

      pid ->
        ref = make_ref()
        GenServer.cast(pid, {:send_command, payload})
        {:ok, ref}
    end
  end

  def http_options(url, timeout) do
    options = [connect_timeout: 2000, timeout: timeout]

    if URI.parse(url).scheme == "https" do
      options ++
        [
          ssl: [
            verify: :verify_peer,
            cacerts: :public_key.cacerts_get(),
            customize_hostname_check: [
              match_fun: :public_key.pkix_verify_hostname_match_fun(:https)
            ]
          ]
        ]
    else
      options
    end
  end

  @impl true
  def init(url), do: {:ok, %{url: url, tasks: %{}}}

  @impl true
  def handle_cast({:send_command, payload}, state) do
    task =
      Task.Supervisor.async_nolink(App.TaskSupervisor, fn ->
        request =
          {String.to_charlist(state.url <> "/commands"), [], ~c"application/json", payload}

        :httpc.request(:post, request, http_options(state.url, 5000), body_format: :binary)
      end)

    {:noreply, %{state | tasks: Map.put(state.tasks, task.ref, task)}}
  end

  @impl true
  def handle_info({ref, result}, state) when is_map_key(state.tasks, ref) do
    Process.demonitor(ref, [:flush])

    case result do
      {:ok, {{_, 204, _}, _, _}} -> Logger.debug("CEC command transmitted")
      other -> Logger.error("CEC command failed: #{inspect(other)}")
    end

    {:noreply, %{state | tasks: Map.delete(state.tasks, ref)}}
  end

  def handle_info({:DOWN, ref, :process, _pid, reason}, state)
      when is_map_key(state.tasks, ref) do
    Logger.error("CEC command task failed: #{inspect(reason)}")
    {:noreply, %{state | tasks: Map.delete(state.tasks, ref)}}
  end

  @impl true
  def terminate(_reason, state) do
    Enum.each(state.tasks, fn {_ref, task} -> Task.shutdown(task, :brutal_kill) end)
    :ok
  end
end
