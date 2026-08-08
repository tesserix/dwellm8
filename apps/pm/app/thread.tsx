import React, { useState } from 'react';
import { View, Text, StyleSheet, TextInput, Pressable, ScrollView, ActivityIndicator } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, ListRow, Avatar, Card, Screen, SendIcon, EmptyState, HouseArt,
  color, font, radius, space, useBack,
} from '@dwellm8/mobile-shared';
import { fmtDate, fmtTime, useOpsThread, useOpsThreads } from '../src/data/worklists';

/** The inbox and one conversation — the manager's side of #238. */

export default function Thread() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();

  if (!id) return <Inbox />;
  return <Conversation leaseId={id} onBack={() => router.push('/thread')} />;
}

function Inbox() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const { loading, error, data: threads } = useOpsThreads();

  return (
    <>
      <BackHeader title="Inbox" onBack={goBack} />
      <Screen>
        {loading ? (
          <View style={{ paddingVertical: space(8), alignItems: 'center' }}><ActivityIndicator /></View>
        ) : error ? (
          <Card><Text style={s.err}>{error}</Text></Card>
        ) : threads.length === 0 ? (
          <EmptyState
            full
            art={<HouseArt size={160} />}
            title="No conversations yet"
            body="When a tenant messages from the Live app, the thread lands here — on the record."
          />
        ) : (
          <Card padded={false} style={{ paddingHorizontal: space(4), marginTop: space(3) }}>
            {threads.map((t, i) => (
              <ListRow
                key={t.lease_id}
                left={<Avatar initials={(t.unit || '?').slice(0, 2).toUpperCase()} tone={t.last_sender === 'resident' ? 'blue' : 'neutral'} />}
                title={`${t.unit}, ${t.property}`}
                subtitle={t.last_body}
                meta={`${t.messages} message${t.messages === 1 ? '' : 's'}`}
                right={<Text style={s.at}>{fmtDate(t.last_at)}</Text>}
                onPress={() => router.push(`/thread?id=${t.lease_id}`)}
                last={i === threads.length - 1}
              />
            ))}
          </Card>
        )}
      </Screen>
    </>
  );
}

function Conversation({ leaseId, onBack }: { leaseId: string; onBack: () => void }) {
  const { loading, data: messages, send } = useOpsThread(leaseId);
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    const body = draft.trim();
    if (!body || busy) return;
    setBusy(true);
    try {
      await send(body);
      setDraft('');
    } finally {
      setBusy(false);
    }
  };

  let lastDay = '';

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <BackHeader title="Conversation" subtitle="On the record" onBack={onBack} />
      {loading ? (
        <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}><ActivityIndicator /></View>
      ) : (
        <ScrollView contentContainerStyle={{ padding: space(4), paddingBottom: space(6) }}>
          {messages.map((m) => {
            const day = fmtDate(m.sent_at);
            const showDay = day !== lastDay;
            lastDay = day;
            const mine = m.sender !== 'resident';
            return (
              <View key={m.message_id}>
                {showDay ? <Text style={s.day}>{day}</Text> : null}
                <View style={[s.bubbleWrap, mine && { alignItems: 'flex-end' }]}>
                  <View style={[s.bubble, mine ? s.mine : s.theirs]}>
                    <Text style={[s.text, mine && { color: '#FFF' }]}>{m.body}</Text>
                  </View>
                  <Text style={s.time}>{fmtTime(m.sent_at)}</Text>
                </View>
              </View>
            );
          })}

          <View style={s.templates}>
            {['On my way', 'Technician arriving within the hour', 'Please share photos'].map((t) => (
              <Pressable key={t} style={s.template} onPress={() => setDraft(t)}>
                <Text style={s.templateText}>{t}</Text>
              </Pressable>
            ))}
          </View>
        </ScrollView>
      )}

      <View style={s.composer}>
        <TextInput
          value={draft}
          onChangeText={setDraft}
          placeholder="Message"
          placeholderTextColor={color.inkFaint}
          style={s.input}
          editable={!busy}
        />
        <Pressable style={[s.send, (!draft.trim() || busy) && { opacity: 0.5 }]} onPress={submit} disabled={!draft.trim() || busy}>
          <SendIcon size={20} c="#FFF" />
        </Pressable>
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  at: { ...font.small, color: color.inkFaint },
  err: { ...font.body, color: color.inkSoft },
  day: { ...font.small, color: color.inkSoft, textAlign: 'center', marginBottom: space(3), marginTop: space(2) },
  bubbleWrap: { marginBottom: space(3) },
  bubble: { maxWidth: '86%', borderRadius: radius.lg, padding: space(3.5) },
  mine: { backgroundColor: color.accent, borderBottomRightRadius: 6 },
  theirs: { backgroundColor: '#FFF', borderBottomLeftRadius: 6 },
  text: { ...font.body, color: color.inkStrong, lineHeight: 21 },
  time: { ...font.small, color: color.inkFaint, marginTop: 4 },
  templates: { flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginTop: space(4) },
  template: {
    backgroundColor: '#FFF', borderRadius: radius.pill, borderWidth: 1, borderColor: color.line,
    paddingHorizontal: 14, paddingVertical: 8,
  },
  templateText: { ...font.small, color: color.accent, fontWeight: '600' },
  composer: {
    flexDirection: 'row', alignItems: 'center', gap: 10, padding: space(3),
    backgroundColor: '#FFF', borderTopWidth: 1, borderTopColor: color.line,
  },
  input: {
    flex: 1, height: 44, borderRadius: radius.pill, backgroundColor: '#F4F7FA',
    paddingHorizontal: space(4), ...font.body, color: color.inkStrong,
  },
  send: { width: 44, height: 44, borderRadius: 22, backgroundColor: color.accent, alignItems: 'center', justifyContent: 'center' },
});
