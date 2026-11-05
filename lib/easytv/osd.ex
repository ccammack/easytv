defmodule Osd do
  use GenServer
  require Logger

  @aosd_args [
    "--position",
    "0",
    "--x-offset",
    "32",
    "--y-offset",
    "32",
    "--padding",
    "16",
    "--transparency",
    "0",
    "--font",
    "monospace 24",
    "--back-color",
    "black",
    "--fore-color",
    "white",
    "--fade-in",
    "0",
    "--fade-full",
    "5000",
    "--fade-out",
    "0",
    "--lines",
    "1"
  ]

  @kill_attempts [
    {"-TERM", 20},
    {"-TERM", 40},
    {"-KILL", 0}
  ]

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def show(message) when is_binary(message) do
    GenServer.cast(__MODULE__, {:show, message})
  end

  def clear do
    GenServer.cast(__MODULE__, :clear)
  end

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)
    {:ok, %{instances: []}}
  end

  @impl true
  def terminate(_reason, state) do
    kill_all(state.instances)
    :ok
  end

  @impl true
  def handle_cast({:show, ""}, state) do
    Logger.debug(
      "osd handle_cast show_empty " <>
        "instances_count=#{length(state.instances)}"
    )

    instances = kill_all(state.instances)

    Logger.debug(
      "osd handle_cast show_empty done " <>
        "remaining_instances=#{length(instances)}"
    )

    {:noreply, %{state | instances: instances}}
  end

  @impl true
  def handle_cast({:show, message}, state) do
    Logger.debug(
      "osd handle_cast show " <>
        "message=#{inspect(message)} " <>
        "instances_count=#{length(state.instances)}"
    )

    instances = kill_all(state.instances)

    Logger.debug(
      "osd handle_cast show after_kill_all " <>
        "message=#{inspect(message)} " <>
        "survivor_count=#{length(instances)}"
    )

    case start_instance(message) do
      {:ok, instance} ->
        Logger.debug(
          "osd start os_pid=#{inspect(instance.os_pid)} pgid=#{inspect(instance.pgid)} message=#{inspect(message)}"
        )

        new_instances = [instance | instances]

        Logger.debug(
          "osd handle_cast show done " <>
            "message=#{inspect(message)} " <>
            "instances_count=#{length(new_instances)}"
        )

        {:noreply, %{state | instances: new_instances}}

      {:error, reason} ->
        Logger.error(
          "osd start failed " <>
            "message=#{inspect(message)} " <>
            "reason=#{inspect(reason)}"
        )

        {:noreply, %{state | instances: instances}}
    end
  end

  @impl true
  def handle_cast(:clear, state) do
    Logger.debug(
      "osd handle_cast clear " <>
        "instances_count=#{length(state.instances)}"
    )

    instances = kill_all(state.instances)

    Logger.debug(
      "osd handle_cast clear done " <>
        "remaining_instances=#{length(instances)}"
    )

    {:noreply, %{state | instances: instances}}
  end

  @impl true
  def handle_info({port, {:exit_status, status}}, state) do
    if status != 0 and Enum.any?(state.instances, &(&1.port == port)) do
      Logger.warning("osd process exited unexpectedly: status=#{status} port=#{inspect(port)}")
    end

    Logger.debug(
      "osd port exit " <>
        "port=#{inspect(port)} " <>
        "status=#{inspect(status)} " <>
        "instances_before=#{length(state.instances)}"
    )

    instances =
      Enum.reject(state.instances, fn instance ->
        instance.port == port
      end)

    Logger.debug(
      "osd port exit pruned " <>
        "port=#{inspect(port)} " <>
        "instances_after=#{length(instances)}"
    )

    {:noreply, %{state | instances: instances}}
  end

  @impl true
  def handle_info({:EXIT, port, reason}, state) when is_port(port) do
    Logger.debug(
      "osd port linked exit " <>
        "port=#{inspect(port)} " <>
        "reason=#{inspect(reason)} " <>
        "instances_before=#{length(state.instances)}"
    )

    instances =
      Enum.reject(state.instances, fn instance ->
        instance.port == port
      end)

    Logger.debug(
      "osd port linked exit pruned " <>
        "port=#{inspect(port)} " <>
        "instances_after=#{length(instances)}"
    )

    {:noreply, %{state | instances: instances}}
  end

  @impl true
  def handle_info(msg, state) do
    Logger.warning("osd handle_info other msg=#{inspect(msg)}")
    {:noreply, state}
  end

  defp start_instance(message) do
    case System.find_executable("aosd_cat") do
      nil ->
        Logger.error("osd start_instance aosd_cat_not_found")
        {:error, :aosd_cat_not_found}

      aosd_cat ->
        Logger.debug(
          "osd start_instance opening_port " <>
            "executable=#{inspect(aosd_cat)} " <>
            "message=#{inspect(message)}"
        )

        port =
          Port.open({:spawn_executable, aosd_cat}, [
            :binary,
            :use_stdio,
            :hide,
            :exit_status,
            {:args, @aosd_args}
          ])

        os_pid =
          case Port.info(port, :os_pid) do
            {:os_pid, pid} when is_integer(pid) -> pid
            _ -> nil
          end

        pgid = get_pgid(os_pid)

        Logger.debug(
          "osd start_instance port_opened " <>
            "port=#{inspect(port)} " <>
            "os_pid=#{inspect(os_pid)} " <>
            "pgid=#{inspect(pgid)} " <>
            "message=#{inspect(message)}"
        )

        case Port.command(port, message <> "\n") do
          true ->
            Logger.debug(
              "osd start_instance command_sent " <>
                "port=#{inspect(port)} " <>
                "message=#{inspect(message)}"
            )

            {:ok, %{port: port, os_pid: os_pid, pgid: pgid, message: message}}

          false ->
            Logger.error(
              "osd start_instance command_failed " <>
                "port=#{inspect(port)} " <>
                "message=#{inspect(message)}"
            )

            safe_close_port(port)
            {:error, :port_command_failed}
        end
    end
  end

  defp kill_all(instances) do
    Logger.debug("osd kill_all begin instances_count=#{length(instances)}")

    survivors =
      instances
      |> Enum.reverse()
      |> Enum.reduce([], fn instance, acc ->
        if kill_instance(instance) do
          acc
        else
          [instance | acc]
        end
      end)

    if survivors != [] do
      Logger.warning("osd cleanup failed: #{length(survivors)} processes remain")
    end

    Logger.debug(
      "osd kill_all done " <>
        "instances_before=#{length(instances)} " <>
        "survivor_count=#{length(survivors)}"
    )

    survivors
  end

  defp kill_instance(%{port: port, os_pid: os_pid, pgid: pgid, message: message} = _instance) do
    target =
      cond do
        is_integer(pgid) and pgid > 0 -> {-pgid, "pgid"}
        is_integer(os_pid) and os_pid > 0 -> {os_pid, "pid"}
        true -> {nil, "none"}
      end

    case target do
      {nil, _} ->
        Logger.warning(
          "osd kill no_target " <>
            "message=#{inspect(message)} " <>
            "port=#{inspect(port)} " <>
            "os_pid=#{inspect(os_pid)} " <>
            "pgid=#{inspect(pgid)}"
        )

        safe_close_port(port)
        true

      {kill_target, target_kind} ->
        Logger.debug(
          "osd kill begin " <>
            "message=#{inspect(message)} " <>
            "target_kind=#{target_kind} " <>
            "target=#{inspect(kill_target)} " <>
            "os_pid=#{inspect(os_pid)} " <>
            "pgid=#{inspect(pgid)}"
        )

        Enum.each(@kill_attempts, fn {signal, sleep_ms} ->
          Logger.debug(
            "osd kill attempt " <>
              "message=#{inspect(message)} " <>
              "signal=#{inspect(signal)} " <>
              "target=#{inspect(kill_target)} " <>
              "sleep_ms=#{inspect(sleep_ms)}"
          )

          _ = System.cmd("kill", [signal, Integer.to_string(kill_target)], stderr_to_stdout: true)

          if sleep_ms > 0 do
            Process.sleep(sleep_ms)
          end
        end)

        dead? =
          cond do
            is_integer(pgid) and pgid > 0 ->
              not alive_by_group?(pgid)

            is_integer(os_pid) and os_pid > 0 ->
              not alive_by_pid?(os_pid)

            true ->
              true
          end

        safe_close_port(port)

        Logger.debug(
          "osd kill result " <>
            "message=#{inspect(message)} " <>
            "dead?=#{inspect(dead?)} " <>
            "os_pid=#{inspect(os_pid)} " <>
            "pgid=#{inspect(pgid)}"
        )

        dead?
    end
  end

  defp alive_by_pid?(pid) when is_integer(pid) and pid > 0 do
    case System.cmd("kill", ["-0", Integer.to_string(pid)], stderr_to_stdout: true) do
      {_out, 0} -> true
      {_out, _status} -> false
    end
  end

  defp alive_by_pid?(_), do: false

  defp alive_by_group?(pgid) when is_integer(pgid) and pgid > 0 do
    case System.cmd("kill", ["-0", "--", Integer.to_string(-pgid)], stderr_to_stdout: true) do
      {_out, 0} -> true
      {_out, _status} -> false
    end
  end

  defp alive_by_group?(_), do: false

  defp get_pgid(pid) when is_integer(pid) and pid > 0 do
    case System.cmd("ps", ["-o", "pgid=", "-p", Integer.to_string(pid)], stderr_to_stdout: true) do
      {output, 0} ->
        output
        |> String.trim()
        |> case do
          "" ->
            nil

          value ->
            case Integer.parse(value) do
              {pgid, ""} -> pgid
              _ -> nil
            end
        end

      _ ->
        nil
    end
  end

  defp get_pgid(_), do: nil

  defp safe_close_port(port) when is_port(port) do
    try do
      Port.close(port)
    rescue
      _ -> :ok
    catch
      _, _ -> :ok
    end
  end

  defp safe_close_port(_), do: :ok
end
