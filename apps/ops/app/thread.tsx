import React, { useState } from 'react';
import { View, Text, StyleSheet, TextInput, Pressable, ScrollView } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import {
  BackHeader, ListRow, Avatar, StatusPill, Card, Screen, SendIcon,
  color, font, radius, space,
} from '@dwellm8/mobile-shared';
import { messages, threads } from '../src/data/mock';

/** The inbox and one conversation. Templates keep replies fast and compliant. */

export default function Thread() {
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id?: string }>();
  const thread = threads.find((t) => t.id === id);
  const [draft, setDraft] = useState('');
  const [sent, setSent] = useState<string[]>([]);

  if (!thread) {
    return (
      <>
        <BackHeader title="Inbox" onBack={() => router.back()} />
        <Screen>
          <Card padded={false} style={{ paddingHorizontal: space(4), marginTop: space(3) }}>
            {threads.map((t, i) => (
              <ListRow
                key={t.id}
                left={<Avatar initials={t.initials} tone={t.unread ? 'blue' : 'neutral'} />}
                title={t.who}
                subtitle={t.preview}
                meta={t.unit}
                right={
                  <View style={{ alignItems: 'flex-end', gap: 6 }}>
                    <Text style={s.at}>{t.at}</Text>
                    {t.unread ? <StatusPill text={String(t.unread)} tone="red" /> : null}
                  </View>
                }
                onPress={() => router.push(`/thread?id=${t.id}`)}
                last={i === threads.length - 1}
              />
            ))}
          </Card>
        </Screen>
      </>
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <BackHeader title={thread.who} subtitle={thread.unit} onBack={() => router.push('/thread')} />
      <ScrollView contentContainerStyle={{ padding: space(4), paddingBottom: space(6) }}>
        <Text style={s.day}>29 July 2026</Text>
        {messages.map((m) => (
          <View key={m.id} style={[s.bubbleWrap, m.mine && { alignItems: 'flex-end' }]}>
            <View style={[s.bubble, m.mine ? s.mine : s.theirs]}>
              <Text style={[s.text, m.mine && { color: '#FFF' }]}>{m.text}</Text>
            </View>
            <Text style={s.time}>{m.at}</Text>
          </View>
        ))}
        {sent.map((t, i) => (
          <View key={`s${i}`} style={[s.bubbleWrap, { alignItems: 'flex-end' }]}>
            <View style={[s.bubble, s.mine]}>
              <Text style={[s.text, { color: '#FFF' }]}>{t}</Text>
            </View>
            <Text style={s.time}>Just now · delivered</Text>
          </View>
        ))}

        <View style={s.templates}>
          {['On my way', 'Technician arriving within the hour', 'Please share photos'].map((t) => (
            <Pressable key={t} style={s.template} onPress={() => setDraft(t)}>
              <Text style={s.templateText}>{t}</Text>
            </Pressable>
          ))}
        </View>
      </ScrollView>

      <View style={s.composer}>
        <TextInput
          value={draft}
          onChangeText={setDraft}
          placeholder="Message"
          placeholderTextColor={color.inkFaint}
          style={s.input}
        />
        <Pressable
          style={s.send}
          onPress={() => {
            if (!draft.trim()) return;
            setSent((x) => [...x, draft.trim()]);
            setDraft('');
          }}
        >
          <SendIcon size={20} c="#FFF" />
        </Pressable>
      </View>
    </View>
  );
}

const s = StyleSheet.create({
  at: { ...font.small, color: color.inkFaint },
  day: { ...font.small, color: color.inkSoft, textAlign: 'center', marginBottom: space(3) },
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
