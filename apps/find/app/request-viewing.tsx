import React, { useEffect, useMemo, useState } from 'react';
import { View, Text, StyleSheet, TextInput, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, Card, Screen, Button, ActionBar, Toast, ChoiceRow, StatusPill, KeyValue,
  color, font, space, type ViewingRequest,
} from '@dwellm8/mobile-shared';
import { api, prospectToken } from '../src/data/source';

/**
 * Ask for a viewing the published times do not cover (#331) — a private one at
 * your own time, or a video walkthrough if you are not in the city.
 *
 * Two open requests per home is the limit, and the API says so by name rather
 * than failing quietly.
 */

const DAY = 86_400_000;

/** The next few mornings and evenings, as times a person would actually offer. */
function options(): { id: string; at: Date }[] {
  const out: { id: string; at: Date }[] = [];
  for (let d = 1; d <= 4; d++) {
    for (const hour of [11, 18]) {
      const at = new Date(Date.now() + d * DAY);
      at.setHours(hour, 0, 0, 0);
      if (at.getTime() > Date.now()) out.push({ id: `${d}-${hour}`, at });
    }
  }
  return out;
}

function whenLabel(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString('en-IN', {
    weekday: 'short', day: '2-digit', month: 'short', hour: 'numeric', minute: '2-digit',
  });
}

export default function RequestViewing() {
  const router = useRouter();
  const { id, headline, kind: kindParam } =
    useLocalSearchParams<{ id?: string; headline?: string; kind?: string }>();
  const client = api();

  const [kind, setKind] = useState<'inspection' | 'online_inspection'>(
    kindParam === 'online_inspection' ? 'online_inspection' : 'inspection');
  const [picked, setPicked] = useState<string[]>([]);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [mine, setMine] = useState<ViewingRequest[] | null>(null);

  const slots = useMemo(options, []);

  const load = async () => {
    if (!client) { setMine([]); return; }
    try {
      const token = await prospectToken(client);
      const all = await client.myViewingRequests(token);
      setMine(id ? all.filter((r) => r.listing_id === id) : all);
    } catch {
      setMine([]);
    }
  };

  useEffect(() => { load(); }, [id]);

  const toggle = (optId: string) => {
    setPicked((was) => was.includes(optId)
      ? was.filter((x) => x !== optId)
      : was.length >= 3 ? was : [...was, optId]);
  };

  const send = async () => {
    if (!client || !id || !picked.length) return;
    setBusy(true);
    try {
      const token = await prospectToken(client);
      await client.requestViewing(token, {
        listingId: id,
        kind,
        times: picked
          .map((p) => slots.find((s) => s.id === p)!.at.toISOString())
          .sort(),
        message: message.trim() || undefined,
      });
      setPicked([]);
      setMessage('');
      setToast('Sent — you will hear back with a time, or another one offered');
      await load();
    } catch (e) {
      setToast(e instanceof Error ? e.message : 'Could not send the request');
    } finally {
      setBusy(false);
    }
  };

  const accept = async (requestId: string, timeId: string) => {
    if (!client) return;
    setBusy(true);
    try {
      const token = await prospectToken(client);
      const b = await client.acceptViewingTime(token, requestId, timeId);
      setToast(b.meeting_link
        ? 'Confirmed — the link is on the request below'
        : 'Confirmed — where to go is on the request below');
      await load();
    } catch (e) {
      setToast(e instanceof Error ? e.message : 'Could not accept that time');
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <BackHeader
        title="Ask for a viewing"
        subtitle={headline ?? 'This home'}
        onBack={() => router.back()}
      />
      <Screen>
        {toast ? <Toast text={toast} /> : null}

        <Card>
          <Text style={s.h}>What would suit you</Text>
          <ChoiceRow
            label="A private viewing"
            hint="Somebody meets you at the property at a time you can make"
            selected={kind === 'inspection'}
            onPress={() => setKind('inspection')}
          />
          <ChoiceRow
            label="A video walkthrough"
            hint="A walk around on a call — for when you are not in the city"
            selected={kind === 'online_inspection'}
            onPress={() => setKind('online_inspection')}
            last
          />
        </Card>

        <Card>
          <Text style={s.h}>Times you can make</Text>
          <Text style={s.body}>Choose up to three. One of them is what gets confirmed.</Text>
          {slots.map((o, idx) => (
            <ChoiceRow
              key={o.id}
              label={whenLabel(o.at.toISOString())}
              selected={picked.includes(o.id)}
              onPress={() => toggle(o.id)}
              last={idx === slots.length - 1}
            />
          ))}
        </Card>

        <Card>
          <Text style={s.h}>Anything they should know</Text>
          <TextInput
            style={s.input}
            value={message}
            onChangeText={setMessage}
            placeholder="I work nights, so weekend mornings are easiest"
            placeholderTextColor={color.inkSoft}
            multiline
          />
        </Card>

        {mine === null ? (
          <Card><ActivityIndicator /></Card>
        ) : mine.length ? (
          <Card>
            <Text style={s.h}>What you have asked for</Text>
            {mine.map((r) => (
              <View key={r.id} style={s.request}>
                <StatusPill
                  text={
                    r.state === 'scheduled' ? 'Confirmed'
                      : r.state === 'closed' ? 'Declined'
                        : r.state === 'owner_responded' ? 'Another time offered'
                          : 'Waiting for an answer'
                  }
                  tone={r.state === 'scheduled' ? 'green' : r.state === 'closed' ? 'red' : 'amber'}
                  dot
                />
                <Text style={s.kind}>
                  {r.kind === 'online_inspection' ? 'Video walkthrough' : 'Private viewing'}
                </Text>
                {r.scheduled_for ? <KeyValue k="When" v={whenLabel(r.scheduled_for)} /> : null}
                {r.meeting_point ? <KeyValue k="Where" v={r.meeting_point} /> : null}
                {r.meeting_link ? <KeyValue k="Link" v={r.meeting_link} /> : null}
                {r.times
                  .filter((t) => t.state === 'open' && t.proposed_by === 'owner')
                  .map((t) => (
                    <View key={t.id} style={{ marginTop: space(2) }}>
                      <Text style={s.body}>{whenLabel(t.starts_at)} was offered instead.</Text>
                      <Button
                        label="Take that time"
                        small
                        disabled={busy}
                        onPress={() => accept(r.id, t.id)}
                        style={{ marginTop: space(2) }}
                      />
                    </View>
                  ))}
                {r.state === 'new' ? (
                  <Text style={s.body}>
                    {r.times.filter((t) => t.state === 'open').map((t) => whenLabel(t.starts_at)).join(' · ')}
                  </Text>
                ) : null}
              </View>
            ))}
          </Card>
        ) : null}
      </Screen>

      <ActionBar>
        <Button label="Back" tone="secondary" onPress={() => router.back()} style={{ flex: 1 }} />
        <Button
          label={busy ? 'Sending…' : 'Send the request'}
          disabled={busy || !picked.length || !client}
          onPress={send}
          style={{ flex: 1.6 }}
        />
      </ActionBar>
    </>
  );
}

const s = StyleSheet.create({
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(1) },
  body: { ...font.body, color: color.inkSoft, marginTop: space(2), lineHeight: 21 },
  kind: { ...font.title, color: color.inkStrong, marginTop: space(2) },
  request: { marginTop: space(4) },
  input: {
    ...font.body, color: color.inkStrong, borderWidth: 1, borderColor: color.line,
    borderRadius: 10, padding: space(3), marginTop: space(2), minHeight: 72,
    textAlignVertical: 'top',
  },
});
