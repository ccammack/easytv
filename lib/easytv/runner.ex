defmodule Runner do
  require Logger

  @task_supervisor App.TaskSupervisor

  def run(cmd, args \\ [], opts \\ [])

  def run(cmd, args, opts) when is_binary(cmd) and is_list(args) and is_list(opts) do
    with :ok <- validate(cmd, args, opts) do
      log? = Keyword.get(opts, :log, true)

      try do
        resolved_cmd =
          case System.find_executable(cmd) do
            nil -> raise ArgumentError, "executable not found: #{cmd}"
            path -> path
          end

        {output, status} =
          System.cmd(resolved_cmd, args, stderr_to_stdout: true)

        result = %{
          cmd: resolved_cmd,
          args: args,
          status: status,
          output: output
        }

        if log? do
          log_result(result)
        end

        {:ok, result}
      rescue
        exception in [ArgumentError, ErlangError] ->
          error(cmd, args, Exception.message(exception))
      end
    else
      {:error, reason} ->
        {:error, reason}
    end
  end

  def run(cmd, args, _opts) do
    error(cmd, args, "invalid arguments to run/3")
  end

  def start(cmd, args \\ [], opts \\ [])

  def start(cmd, args, opts) when is_binary(cmd) and is_list(args) and is_list(opts) do
    case Process.whereis(@task_supervisor) do
      nil ->
        Logger.error(
          "runner start failed cmd=#{inspect(cmd)} args=#{inspect(args)} reason=:task_supervisor_not_started"
        )

        {:error, :task_supervisor_not_started}

      _pid ->
        case Task.Supervisor.start_child(@task_supervisor, fn ->
               _ = run(cmd, args, opts)
               :ok
             end) do
          {:ok, _pid} ->
            :ok

          {:error, reason} ->
            Logger.error(
              "runner start failed cmd=#{inspect(cmd)} args=#{inspect(args)} reason=#{inspect(reason)}"
            )

            {:error, reason}
        end
    end
  end

  def start(cmd, args, _opts) do
    error(cmd, args, "invalid arguments to start/3")
  end

  defp validate(cmd, args, opts) do
    cond do
      not Enum.all?(args, &is_binary/1) ->
        error(cmd, args, "args must be a list of binaries")

      not Keyword.keyword?(opts) ->
        error(cmd, args, "opts must be a keyword list")

      not is_boolean(Keyword.get(opts, :log, true)) ->
        error(cmd, args, ":log must be a boolean")

      true ->
        :ok
    end
  end

  defp error(cmd, args, message) do
    reason = %{
      cmd: cmd,
      args: args,
      exception: message
    }

    Logger.error(
      "runner exception cmd=#{inspect(cmd)} args=#{inspect(args)} exception=#{inspect(message)}"
    )

    {:error, reason}
  end

  defp log_result(%{cmd: cmd, args: args, status: status, output: output}) do
    trimmed = String.trim(output)

    level =
      case status do
        0 -> :debug
        _ -> :error
      end

    message =
      if trimmed == "" do
        "command=#{inspect(cmd)} args=#{inspect(args)} status=#{status}"
      else
        "command=#{inspect(cmd)} args=#{inspect(args)} status=#{status} output=#{trimmed}"
      end

    Logger.log(level, message)
  end
end
