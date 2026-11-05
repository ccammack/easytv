defmodule EasyTV.CECObservations do
  require Logger

  def interpret(%{type: :observation_gap} = event), do: event

  def interpret(%{"event" => "cec_message_received"} = packet) do
    adapter = Map.get(packet, "adapter")

    cond do
      packet["adapter_error"] not in [nil, ""] ->
        observation(:observation_gap, packet, adapter)

      packet["validation_error"] not in [nil, ""] ->
        observation(:invalid_packet, packet, adapter)

      true ->
        case decode(packet) do
          {:ok, bytes} ->
            observation(classify(bytes, identity(adapter)), packet, adapter)

          :error ->
            observation(:invalid_packet, packet, adapter)
        end
    end
  end

  def interpret(%{"event" => event} = packet)
      when event in [
             "adapter_state_changed",
             "cec_messages_lost",
             "cec_adapter_event",
             "cec_capture_error"
           ],
      do: observation(:observation_gap, packet, nil)

  def interpret(packet), do: observation(:unhandled_event, packet, nil)

  defp observation(type, packet, adapter) do
    if type in [:invalid_packet, :observation_gap] do
      Logger.warning("CEC #{type}: #{inspect(packet)}")
    end

    %{type: type, packet: packet, adapter: adapter}
  end

  defp decode(%{"raw" => raw, "length" => length}) when is_binary(raw) do
    case Base.decode16(raw, case: :mixed) do
      {:ok, bytes} when length in 1..16 and byte_size(bytes) == length -> {:ok, bytes}
      _ -> :error
    end
  end

  defp decode(_), do: :error

  defp identity(%{"physical_address" => path, "logical_addresses" => addresses})
       when is_binary(path) and is_list(addresses) do
    logical = Enum.filter(addresses, &(is_integer(&1) and &1 in 1..14))

    if valid_path?(path) and path != "0.0.0.0" and logical != [] do
      {path, logical}
    end
  end

  defp identity(_), do: nil

  defp valid_path?(path) do
    case String.split(path, ".") do
      [a, b, c, d] = parts ->
        Enum.all?(parts, &(&1 in ~w(0 1 2 3 4 5 6 7 8 9 a b c d e f))) and
          parts != ~w(f f f f) and
          (a != "0" or [b, c, d] == ~w(0 0 0)) and
          (b != "0" or [c, d] == ~w(0 0)) and (c != "0" or d == "0")

      _ ->
        false
    end
  end

  defp path(bytes) do
    for <<nibble::4 <- bytes>>, do: String.downcase(Integer.to_string(nibble, 16))
  end

  defp classify(<<0::4, destination::4, 0x36>>, identity),
    do: standby(destination, identity)

  defp classify(<<0::4, destination::4, 0x90, power>>, identity) when power in [1, 3],
    do: standby(destination, identity)

  defp classify(_bytes, nil), do: :observation_gap

  defp classify(<<sender::4, 15::4, 0x82, address::binary-size(2)>>, {own_path, logical}) do
    announced = Enum.join(path(address), ".")

    cond do
      not valid_path?(announced) -> :invalid_packet
      sender == 15 -> :invalid_packet
      sender in logical and announced == own_path -> :focus_here
      sender in logical or announced == own_path -> :unhandled_packet
      true -> :focus_lost
    end
  end

  defp classify(<<0x0F, 0x86, address::binary-size(2)>>, {own_path, _logical}) do
    if Enum.join(path(address), ".") == own_path,
      do: :tv_selected,
      else: :unhandled_packet
  end

  defp classify(_bytes, _identity), do: :unhandled_packet

  defp standby(15, _identity), do: :tv_standby
  defp standby(_destination, nil), do: :observation_gap

  defp standby(destination, {_path, logical}) do
    if destination in logical, do: :tv_standby, else: :unhandled_packet
  end
end
