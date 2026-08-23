// SPDX-License-Identifier: GPL-3.0-or-later
using System.Text.Json;

namespace Bindery.Ra2.Adapter;

public sealed class BoundedEventSpool<T>(int capacity)
{
    private readonly object gate = new();
    private readonly Queue<T> queue = new();

    public int Count { get { lock (gate) return queue.Count; } }
    public long Dropped { get; private set; }

    public bool TryAppend(T value)
    {
        lock (gate)
        {
            if (queue.Count >= capacity) { Dropped++; return false; }
            queue.Enqueue(value);
            return true;
        }
    }

    public IReadOnlyList<T> Drain(int maximum)
    {
        if (maximum <= 0) return [];
        lock (gate)
        {
            List<T> result = new(Math.Min(maximum, queue.Count));
            while (result.Count < maximum && queue.Count > 0) result.Add(queue.Dequeue());
            return result;
        }
    }

    public static string SerializeBatch(IReadOnlyList<T> events) => JsonSerializer.Serialize(events);
}

