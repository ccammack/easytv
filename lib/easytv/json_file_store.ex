defmodule JsonFileStore do
  def load!(path, default, validate!, label)
      when is_binary(path) and is_function(validate!, 1) and is_binary(label) do
    case File.read(path) do
      {:ok, contents} ->
        case Jason.decode(contents) do
          {:ok, data} ->
            validate!.(data)

          {:error, reason} ->
            raise "Could not parse #{label} #{path}: #{inspect(reason)}"
        end

      {:error, :enoent} ->
        default

      {:error, reason} ->
        raise "Could not read #{label} #{path}: #{inspect(reason)}"
    end
  end

  def save!(path, data, validate!) when is_binary(path) and is_function(validate!, 1) do
    validate!.(data)
    dir = Path.dirname(path)
    File.mkdir_p!(dir)

    json = Jason.encode_to_iodata!(data, pretty: true)
    tmp = path <> ".tmp"

    File.write!(tmp, json)

    case File.rename(tmp, path) do
      :ok ->
        :ok

      {:error, reason} ->
        # Log and keep going instead of crashing the whole app
        IO.puts("JsonFileStore.save!: rename failed from #{tmp} to #{path}: #{inspect(reason)}")

        # Best-effort fallback: try a second time after re-creating dir
        File.mkdir_p!(dir)

        case File.rename(tmp, path) do
          :ok ->
            :ok

          {:error, reason2} ->
            IO.puts(
              "JsonFileStore.save!: second rename failed from #{tmp} to #{path}: #{inspect(reason2)}"
            )

            # Leave the .tmp file; caller can decide if this is fatal
            :error
        end
    end
  end
end
