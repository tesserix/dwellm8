import React, { useRef, useState } from 'react';
import { View, Text, StyleSheet, Pressable, TextInput, ScrollView, ActivityIndicator } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { AppHeader, AvatarButton, EmptyState, HouseArt, SendIcon, color, font, radius, shadow, space } from '@dwellm8/mobile-shared';
import { useLiveData, useThread } from '../../src/data/source';

/** The tenancy conversation, on the record (#246). */
export default function Chat() {
  const router = useRouter();
  const { tenancy, leaseId } = useLiveData();
  const { loading, messages, send } = useThread(leaseId);
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const scroll = useRef<ScrollView>(null);
  let lastDay = '';

  const submit = async () => {
    const body = draft.trim();
    if (!body || busy) return;
    setBusy(true);
    try {
      await send(body);
      setDraft('');
      scroll.current?.scrollToEnd({ animated: true });
    } finally {
      setBusy(false);
    }
  };

  return (
    <View style={{ flex: 1, backgroundColor: '#F7FAFC' }}>
      <AppHeader
        title="Messages"
        showCaret={false}
        left={<AvatarButton onPress={() => router.push('/profile')} />}
      />
      {tenancy.agency ? (
        <View style={s.sub}><Text style={s.subText}>{tenancy.agency}</Text></View>
      ) : null}

      {loading ? (
        <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center' }}><ActivityIndicator /></View>
      ) : messages.length === 0 ? (
        <EmptyState
          art={<HouseArt size={160} />}
          title="Start the conversation"
          body="Everything you and your manager agree here stays on the record — repair updates, visit slots, notices."
        />
      ) : (
        <ScrollView
          ref={scroll}
          contentContainerStyle={{ padding: space(4), paddingBottom: space(6) }}
          onContentSizeChange={() => scroll.current?.scrollToEnd({ animated: false })}
        >
          {messages.map((m) => {
            const showDay = m.day !== lastDay;
            lastDay = m.day;
            return (
              <View key={m.id}>
                {showDay ? (
                  <View style={s.dayWrap}>
                    <View style={s.dayLine} />
                    <View style={s.dayPill}><Text style={s.dayText}>{m.day}</Text></View>
                    <View style={s.dayLine} />
                  </View>
                ) : null}
                <View style={[s.row, m.mine ? { justifyContent: 'flex-end' } : null]}>
                  {!m.mine ? <View style={s.dot}><Text style={s.dotText}>M</Text></View> : null}
                  <View style={[s.bubble, m.mine ? s.mine : s.theirs]}>
                    <Text style={[s.msg, m.mine && { color: '#FFF' }]}>{m.text}</Text>
                  </View>
                </View>
                <Text style={[s.time, m.mine ? { textAlign: 'right' } : { marginLeft: 46 }]}>{m.at}</Text>
              </View>
            );
          })}
        </ScrollView>
      )}

      <SafeAreaView edges={['bottom']} style={s.composerSafe}>
        <View style={s.composer}>
          <TextInput
            style={s.input}
            placeholder="Message"
            placeholderTextColor={color.inkFaint}
            value={draft}
            onChangeText={setDraft}
            multiline
            editable={!busy && !!leaseId}
          />
          <Pressable hitSlop={8} onPress={submit} disabled={!draft.trim() || busy}>
            <SendIcon size={28} c={draft.trim() && !busy ? color.accent : '#BBD3E0'} />
          </Pressable>
        </View>
      </SafeAreaView>
    </View>
  );
}

const s = StyleSheet.create({
  sub: { backgroundColor: '#FFF', paddingBottom: space(3), alignItems: 'center' },
  subText: { ...font.small, color: color.inkSoft },
  dayWrap: { flexDirection: 'row', alignItems: 'center', gap: 10, marginVertical: space(4) },
  dayLine: { flex: 1, height: 1, backgroundColor: color.line },
  dayPill: {
    backgroundColor: '#FFF', borderRadius: radius.pill, borderWidth: 1, borderColor: color.line,
    paddingHorizontal: 16, paddingVertical: 7,
  },
  dayText: { ...font.label, color: color.ink },
  row: { flexDirection: 'row', alignItems: 'flex-end', gap: 8 },
  dot: { width: 34, height: 34, borderRadius: 17, backgroundColor: '#DCE9F2', alignItems: 'center', justifyContent: 'center' },
  dotText: { ...font.tiny, color: color.accentDeep },
  bubble: { maxWidth: '80%', borderRadius: 18, padding: space(4) },
  mine: { backgroundColor: '#3E5C96', borderBottomRightRadius: 4 },
  theirs: { backgroundColor: '#DCE6F6', borderBottomLeftRadius: 4 },
  msg: { ...font.body, color: color.inkStrong, lineHeight: 22 },
  time: { ...font.small, color: color.inkFaint, marginTop: 5, marginBottom: space(3) },
  composerSafe: { backgroundColor: '#FFF', ...shadow.bar },
  composer: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: space(4), paddingVertical: space(3) },
  input: {
    flex: 1, borderWidth: 1, borderColor: color.line, borderRadius: radius.pill,
    paddingHorizontal: space(4), paddingVertical: space(3), maxHeight: 110, ...font.body, color: color.inkStrong,
  },
});
