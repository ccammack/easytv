defmodule Script do
  def name, do: "easytv"

  def hook_path(file), do: Path.join(to_string(:code.priv_dir(:easytv)), file)

  def configure_paths!(opts) do
    state_dir = expand_dir!(Keyword.get(opts, :state_dir, ".easytv/state"))
    temp_dir = expand_dir!(Keyword.get(opts, :temp_dir, ".easytv/tmp"))
    validate_socket_path!(Path.join(temp_dir, "mpv-18446744073709551615.sock"))
    Application.put_env(:easytv, :state_dir, state_dir)
    Application.put_env(:easytv, :temp_dir, temp_dir)
  end

  defp expand_dir!(""), do: raise(ArgumentError, "storage directory must not be empty")
  defp expand_dir!(path), do: Path.expand(path)

  def validate_socket_path!(path) do
    if byte_size(path) > 107 do
      raise ArgumentError,
            "mpv IPC socket path exceeds Linux's 107-byte limit; use --temp-dir with a shorter path: #{path}"
    end

    path
  end

  def state_dir, do: Application.get_env(:easytv, :state_dir, Path.expand(".easytv/state"))
  def temp_dir, do: Application.get_env(:easytv, :temp_dir, Path.expand(".easytv/tmp"))
  def state_path(file), do: Path.join(state_dir(), file)

  def tmp_file(suffix) when is_binary(suffix) do
    tmp_file([suffix])
  end

  def tmp_file(segments) when is_list(segments) do
    path = Path.join([temp_dir() | segments])
    File.mkdir_p!(Path.dirname(path))
    path
  end

  def tmp_dir(suffix) when is_binary(suffix) do
    tmp_dir([suffix])
  end

  def tmp_dir(segments) when is_list(segments) do
    dir = Path.join([temp_dir() | segments])
    File.mkdir_p!(dir)
    dir
  end
end
